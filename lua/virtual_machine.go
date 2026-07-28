package lua

import (
	"fmt"
	"os"
	"path/filepath"

	scriptEngine "github.com/tx7do/go-scripts"

	"github.com/tengattack/gluacrypto"
	luahttp "github.com/vadv/gopher-lua-libs/http/client"
	luajson "github.com/vadv/gopher-lua-libs/json"
	"github.com/yuin/gluamapper"
	Lua "github.com/yuin/gopher-lua"
	luar "layeh.com/gopher-luar"
)

// TableMap is a convenience alias for maps exchanged between Go and Lua.
type TableMap map[string]interface{}

// virtualMachine wraps a Lua LState and the currently-loaded function.
//
// ownsState reports whether the VM created its LState itself (sandbox mode). A
// pooled LState (ownsState == false) is returned to the pool on Destroy and may
// be reused by other engines; a privately-owned one is closed outright so it
// can never leak sandboxed-vs-full-lib state across engines.
type virtualMachine struct {
	L        *Lua.LState
	F        *Lua.LFunction
	ownsState bool
}

// newVirtualMachine obtains a Lua state and initializes it.
//
// When openLibs is non-empty the VM runs in sandbox mode: it creates its OWN
// private LState (with SkipOpenLibs so no standard library is opened), then
// opens only the allow-listed libraries. A private state guarantees the
// allow-list is authoritative — it cannot be polluted by a previously pooled,
// full-library state reused by some other engine. The state is closed on
// Destroy (it is not returned to any pool).
//
// When openLibs is empty the VM borrows a state from the shared pool and opens
// the full standard-library set (the default behavior).
func newVirtualMachine(openLibs []string) *virtualMachine {
	var L *Lua.LState
	ownsState := false
	if len(openLibs) > 0 {
		// Sandbox mode: a fresh, library-less state we fully control.
		L = Lua.NewState(Lua.Options{
			CallStackSize:       4096,
			RegistrySize:        4096,
			SkipOpenLibs:        true,
			IncludeGoStackTrace: true,
		})
		ownsState = true
	} else {
		// Default mode: reuse a pooled state for performance.
		L = luaPool.Borrow()
	}
	exec := &virtualMachine{
		L:         L,
		ownsState: ownsState,
	}
	exec.init(openLibs)
	return exec
}

// GetRunPath returns the absolute directory of the current executable.
func GetRunPath() string {
	path, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	return path
}

// init opens the standard Lua libs and preloads extras (json, http client,
// crypto, ...) and registers a GetLuaPath helper.
//
// When openLibs is non-empty, only the listed standard libraries are opened
// (sandbox mode); libraries not listed — e.g. os / io / debug — are NOT opened,
// preventing scripts from escaping the host. An empty/nil openLibs opens the
// full standard-library set via OpenLibs().
func (e *virtualMachine) init(openLibs []string) {

	packageOpened := e.openLuaStandardLibs(openLibs)

	// Extra modules register through package.preload (L.PreloadModule), which
	// requires the `package` standard library to be open — without it,
	// PreloadModule indexes nil and panics. In sandbox mode without `package`
	// allow-listed we skip these preloads: a script without `require` cannot
	// reach them anyway. When `package` IS open (the default, or explicitly
	// allow-listed) the extras preload as usual.
	//
	// NOTE: we preload ONLY the modules actually used (json, http client, crypto)
	// instead of the aggregate libs.Preload(). The latter drags in the entire
	// gopher-lua-libs ecosystem — most notably http/server, which transitively
	// imports aws/cloudwatch, db, telegram, prometheus, chef, zabbix, ... and
	// pulls in aws-sdk-go and (via db) the CGO go-sqlite3. Preloading just these
	// three keeps the dependency graph clean and CGO-free.
	if packageOpened {
		luajson.Preload(e.L)
		luahttp.Preload(e.L)
		gluacrypto.Preload(e.L)
	}

	//lua_debugger.Preload(e.L)

	e.RegisterFunction("GetLuaPath", func(vm *Lua.LState) int {
		// absolute path of the executable's directory
		e.L.Push(Lua.LString(GetRunPath() + "/script"))
		return 1
	})
}

// openLuaStandardLibs opens the standard Lua libraries. When allowList is empty
// it opens the full set; otherwise only the named libraries are opened. It
// returns whether the `package` library was opened (callers use this to decide
// whether module preloads — which rely on package.preload — are safe).
func (e *virtualMachine) openLuaStandardLibs(allowList []string) bool {
	if len(allowList) == 0 {
		e.L.OpenLibs()
		return true // OpenLibs always opens package.
	}

	// Build the set of allow-listed names for O(1) lookup; unknown names are
	// ignored (they neither open a library nor cause an error).
	allowed := make(map[string]bool, len(allowList))
	for _, name := range allowList {
		allowed[name] = true
	}

	// package and base must load before the others; iterate stdLibOpeners in
	// its declaration order so the relative ordering is preserved and only the
	// allow-listed ones run.
	packageOpened := false
	for _, l := range stdLibOpeners {
		if allowed[l.name] {
			if l.name == Lua.LoadLibName {
				packageOpened = true
			}
			e.L.Push(e.L.NewFunction(l.opener))
			e.L.Push(Lua.LString(l.name))
			e.L.Call(1, 0)
		}
	}
	return packageOpened
}

// stdLibOpener pairs a standard-library name with its gopher-lua opener.
type stdLibOpener struct {
	name   string
	opener Lua.LGFunction
}

// stdLibOpeners is the full set of gopher-lua standard libraries, in the order
// gopher-lua's OpenLibs opens them (package and base first). Used both to open
// the full set and to drive the allow-list.
var stdLibOpeners = []stdLibOpener{
	{name: Lua.LoadLibName, opener: Lua.OpenPackage},
	{name: Lua.BaseLibName, opener: Lua.OpenBase},
	{name: Lua.TabLibName, opener: Lua.OpenTable},
	{name: Lua.IoLibName, opener: Lua.OpenIo},
	{name: Lua.OsLibName, opener: Lua.OpenOs},
	{name: Lua.StringLibName, opener: Lua.OpenString},
	{name: Lua.MathLibName, opener: Lua.OpenMath},
	{name: Lua.DebugLibName, opener: Lua.OpenDebug},
	{name: Lua.ChannelLibName, opener: Lua.OpenChannel},
	{name: Lua.CoroutineLibName, opener: Lua.OpenCoroutine},
}

// Destroy releases the Lua state. A privately-owned (sandbox) state is Closed
// outright so it can never be reused; a pooled state is returned to the pool.
func (e *virtualMachine) Destroy() {
	if e.L == nil {
		return
	}
	if e.ownsState {
		e.L.Close()
	} else {
		luaPool.Return(e.L)
	}
}

// LoadString compiles a string source into a Lua function.
func (e *virtualMachine) LoadString(source string) error {
	var lFunc *Lua.LFunction
	var err error
	if lFunc, err = e.L.LoadString(source); err != nil {
		return err
	}

	e.F = lFunc

	return nil
}

// LoadFile compiles the file at the given path into a Lua function.
func (e *virtualMachine) LoadFile(filePath string) error {
	var lFunc *Lua.LFunction
	var err error
	if lFunc, err = e.L.LoadFile(filePath); err != nil {
		return err
	}

	e.F = lFunc

	return nil
}

// Execute runs the previously-compiled Lua function.
func (e *virtualMachine) Execute() error {
	if err := e.doCompiledFile(); err != nil {
		return err
	}
	return nil
}

// ExecuteString immediately runs the given string source.
func (e *virtualMachine) ExecuteString(source string) error {
	if err := e.L.DoString(source); err != nil {
		return err
	}
	return nil
}

// ExecuteFile immediately runs the script at the given file path.
func (e *virtualMachine) ExecuteFile(filePath string) error {
	if err := e.L.DoFile(filePath); err != nil {
		return err
	}
	return nil
}

// CallFunction invokes a global Lua function by name. Panics on error.
func (e *virtualMachine) CallFunction(name string, args ...interface{}) {
	var lArgs []Lua.LValue
	for _, arg := range args {
		lArgs = append(lArgs, e.convertToLValue(arg))
	}

	if err := e.L.CallByParam(Lua.P{
		Fn:      e.L.GetGlobal(name),
		NRet:    1,    // number of return values
		Protect: true, // return error instead of panic on failure
	}, lArgs...); err != nil { // input arguments
		panic(err)
	}
}

// PCall calls the named global function via pcall; errors are logged, not returned.
func (e *virtualMachine) PCall(f string, args ...interface{}) {
	e.L.Push(e.L.GetGlobal(f))
	for _, arg := range args {
		val := e.convertToLValue(arg)
		e.L.Push(val)
	}
	if err := e.L.PCall(len(args), -1, nil); err != nil {
		scriptEngine.GetLogger().Error(nil, "lua pcall error", "func", f, "err", err)
	}
}

// PCall2 is like PCall but accepts pre-converted LValue arguments.
func (e *virtualMachine) PCall2(f string, args ...Lua.LValue) {
	e.L.Push(e.L.GetGlobal(f))
	for _, arg := range args {
		e.L.Push(arg)
	}
	if err := e.L.PCall(len(args), -1, nil); err != nil {
		scriptEngine.GetLogger().Error(nil, "lua pcall2 error", "func", f, "err", err)
	}
}

// PCall3 is like PCall2 but accepts an LValue callable directly (not a name).
func (e *virtualMachine) PCall3(f Lua.LValue, args ...Lua.LValue) {
	e.L.Push(f)
	for _, arg := range args {
		e.L.Push(arg)
	}
	if err := e.L.PCall(len(args), -1, nil); err != nil {
		scriptEngine.GetLogger().Error(nil, "lua pcall3 error", "err", err)
	}
}

// RegisterFunction registers a Go function as a global Lua function.
func (e *virtualMachine) RegisterFunction(name string, fn Lua.LGFunction) {
	e.L.SetGlobal(name, e.L.NewFunction(fn))
}

// RegisterModule registers a Lua module loader under name.
func (e *virtualMachine) RegisterModule(name string, mod Lua.LGFunction) {
	e.L.Push(e.L.NewFunction(mod))
	e.L.Push(Lua.LString(name))
	e.L.Call(1, 0)
}

// BindStruct binds a Go struct (or any value) to a Lua global, allowing both
// sides to read/write it.
func (e *virtualMachine) BindStruct(name string, data interface{}) {
	e.L.SetGlobal(name, luar.New(e.L, data))
}

// GetLuaTableToStruct maps a Lua table into the given Go struct via gluamapper.
func (e *virtualMachine) GetLuaTableToStruct(name string, out interface{}) error {
	return gluamapper.Map(e.L.GetGlobal(name).(*Lua.LTable), &out)
}

// doCompiledFile runs the currently-loaded compiled function.
func (e *virtualMachine) doCompiledFile() error {
	e.L.Push(e.F)
	return e.L.PCall(0, Lua.MultRet, nil)
}

// convertToLValue converts a Go value into a Lua LValue.
func (e *virtualMachine) convertToLValue(val interface{}) Lua.LValue {
	if val == nil {
		return Lua.LNil
	}
	switch v := val.(type) {
	case Lua.LValue:
		return v
	case bool:
		return Lua.LBool(v)
	case float32:
		return Lua.LNumber(v)
	case float64:
		return Lua.LNumber(v)
	case int:
		return Lua.LNumber(v)
	case int8:
		return Lua.LNumber(v)
	case int16:
		return Lua.LNumber(v)
	case int32:
		return Lua.LNumber(v)
	case int64:
		return Lua.LNumber(v)
	case uint8:
		return Lua.LNumber(v)
	case uint16:
		return Lua.LNumber(v)
	case uint32:
		return Lua.LNumber(v)
	case uint64:
		return Lua.LNumber(v)
	case string:
		return Lua.LString(v)
	case []byte:
		ud := e.L.NewUserData()
		ud.Value = v
		return ud
	case map[string]interface{}:
		return e.convertToLTable(v)
	case []interface{}:
		lt := e.L.NewTable()
		for k, v := range v {
			lt.RawSetInt(k+1, e.convertToLValue(v))
		}
		return lt
	default:
		return nil
	}
}

// convertFromLValue converts a Lua LValue into a Go value.
func (e *virtualMachine) convertFromLValue(lv Lua.LValue) interface{} {
	switch v := lv.(type) {
	case *Lua.LNilType:
		return nil
	case *Lua.LUserData:
		return v.Value
	case Lua.LBool:
		return bool(v)
	case Lua.LString:
		return string(v)
	case Lua.LNumber:
		f64i := float64(v)
		I64i := int64(v)
		if f64i == float64(I64i) {
			return I64i
		}
		return f64i
	case *Lua.LTable:
		maxN := v.MaxN()
		if maxN == 0 {
			// map-style table
			ret := make(map[string]interface{})
			v.ForEach(func(key, value Lua.LValue) {
				keyStr := fmt.Sprint(e.convertFromLValue(key))
				ret[keyStr] = e.convertFromLValue(value)
			})
			return ret
		} else {
			// array-style table
			ret := make([]interface{}, 0, maxN)
			for i := 1; i <= maxN; i++ {
				ret = append(ret, e.convertFromLValue(v.RawGetInt(i)))
			}
			return ret
		}
	default:
		scriptEngine.GetLogger().Error(nil, "unsupported lua type", "type", fmt.Sprintf("%T", lv), "value", lv)
		return nil
	}
}

// convertToLTable converts a Go map into a Lua LTable.
func (e *virtualMachine) convertToLTable(data map[string]interface{}) *Lua.LTable {
	lt := e.L.NewTable()

	for k, v := range data {
		lt.RawSetString(k, e.convertToLValue(v))
	}

	return lt
}

// convertFromLTable converts a Lua LTable into a Go map.
func (e *virtualMachine) convertFromLTable(lv *Lua.LTable) map[string]interface{} {
	returnData, _ := e.convertFromLValue(lv).(map[string]interface{})
	return returnData
}
