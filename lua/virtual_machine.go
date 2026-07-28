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

// virtualMachine wraps a Lua PState and the currently-loaded function.
type virtualMachine struct {
	L *Lua.LState
	F *Lua.LFunction

	// openLibs controls which standard libraries are opened during init. A nil
	// or empty slice means "open all libraries" (backward-compatible default).
	// When set, only the named libraries are opened; this is the sandbox mode
	// that lets callers drop dangerous libraries such as os/io/package. See
	// AllowedLib* constants and AllowedLibs for valid names.
	openLibs []string
}

// newVirtualMachine borrows a Lua state from the pool and initializes it.
// openLibs selects which standard libraries to open: nil/empty opens all
// (backward-compatible); a non-empty list opens only those (sandbox mode).
func newVirtualMachine(openLibs []string) *virtualMachine {
	exec := &virtualMachine{
		L:        luaPool.Borrow(),
		openLibs: openLibs,
	}
	exec.init()
	return exec
}

// GetRunPath returns the absolute directory of the current executable.
func GetRunPath() string {
	path, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	return path
}

// Standard-library names recognized by the sandbox whitelist. They mirror the
// Lua/gopher-lua library namespaces. Use them with engine.SetOpenLibs.
const (
	// AllowedLibBase is the base library (print, pairs, require, error, ...).
	// Note: gopher-lua ties the "require" loader (package) into the base lib's
	// OpenBase; keeping Base enabled keeps require usable.
	AllowedLibBase = Lua.BaseLibName
	// AllowedLibLoad is the package library (require / module loaders).
	AllowedLibLoad      = Lua.LoadLibName
	AllowedLibTab       = Lua.TabLibName
	AllowedLibIo        = Lua.IoLibName
	AllowedLibOs        = Lua.OsLibName
	AllowedLibStr       = Lua.StringLibName
	AllowedLibMath      = Lua.MathLibName
	AllowedLibDebug     = Lua.DebugLibName
	AllowedLibChannel   = Lua.ChannelLibName
	AllowedLibCoroutine = Lua.CoroutineLibName
)

// stdLibOpeners maps each standard-library name to its gopher-lua opener. A
// nil entry (or empty openLibs) means "open everything".
var stdLibOpeners = map[string]Lua.LGFunction{
	Lua.BaseLibName:      Lua.OpenBase,
	Lua.LoadLibName:      Lua.OpenPackage,
	Lua.TabLibName:       Lua.OpenTable,
	Lua.IoLibName:        Lua.OpenIo,
	Lua.OsLibName:        Lua.OpenOs,
	Lua.StringLibName:    Lua.OpenString,
	Lua.MathLibName:      Lua.OpenMath,
	Lua.DebugLibName:     Lua.OpenDebug,
	Lua.ChannelLibName:   Lua.OpenChannel,
	Lua.CoroutineLibName: Lua.OpenCoroutine,
}

// init opens the standard Lua libs and preloads extras (gopher-lua-libs,
// gluacrypto, ...) and registers a GetLuaPath helper. This establishes the
// engine's built-in globals; business globals injected by RuntimeHooks are
// tracked separately by the engine so they can be cleared before the LState is
// returned to the pool.
func (e *virtualMachine) init() {
	e.initBuiltinLibs()
}

// initBuiltinLibs opens the standard Lua libs and preloads extras
// (gopher-lua-libs, gluacrypto, ...) and registers a GetLuaPath helper.
//
// When openLibs is empty, all standard libraries are opened (the original
// behavior — backward compatible). When it lists specific library names (see
// AllowedLib* constants), only those are opened; this is the sandbox mode that
// lets callers drop dangerous libraries such as os/io.
//
// Note: the gopher-lua-libs extensions and gluacrypto are preloaded via
// require and only depend on package loaders; they are registered regardless
// of the whitelist (a script still needs require/package enabled to actually
// load them).
func (e *virtualMachine) initBuiltinLibs() {
	if len(e.openLibs) == 0 {
		e.L.OpenLibs()
	} else {
		e.openAllowedLibs()
	}

	// Preload ONLY the gopher-lua-libs modules actually used (json, http client,
	// crypto) instead of the aggregate libs.Preload(). The latter pulls in the
	// whole ecosystem — most notably http/server, which transitively imports
	// aws/cloudwatch, db, telegram, prometheus, chef, zabbix, ... and drags in
	// aws-sdk-go and (via db) the CGO go-sqlite3. These extensions register via
	// require/package.preload and only need the package loaders, so they are
	// registered regardless of the sandbox whitelist; a script still needs
	// require/package enabled to actually load them.
	luajson.Preload(e.L)
	luahttp.Preload(e.L)

	gluacrypto.Preload(e.L)

	//lua_debugger.Preload(e.L)

	e.RegisterFunction("GetLuaPath", func(vm *Lua.LState) int {
		// absolute path of the executable's directory
		e.L.Push(Lua.LString(GetRunPath() + "/script"))
		return 1
	})
}

// openAllowedLibs opens only the standard libraries named in openLibs, in the
// gopher-lua expected order (Load/Base first, then the rest), and removes every
// other standard library from the global scope. Removing is necessary because
// the LState may be recycled from the pool and carry libraries loaded by a
// previous owner that ran with all libraries open; a sandbox owner must not
// inherit those. Unknown names in openLibs are silently ignored.
func (e *virtualMachine) openAllowedLibs() {
	allowed := make(map[string]struct{}, len(e.openLibs))
	for _, name := range e.openLibs {
		allowed[name] = struct{}{}
	}

	// gopher-lua requires Load (package) and Base to open before the rest,
	// because their openers set up _G and require.
	open := func(name string) {
		opener, ok := stdLibOpeners[name]
		if !ok {
			return
		}
		e.L.Push(e.L.NewFunction(opener))
		e.L.Push(Lua.LString(name))
		e.L.Call(1, 0)
	}

	if _, ok := allowed[Lua.LoadLibName]; ok {
		open(Lua.LoadLibName)
	}
	if _, ok := allowed[Lua.BaseLibName]; ok {
		open(Lua.BaseLibName)
	}
	for name := range stdLibOpeners {
		if name == Lua.LoadLibName || name == Lua.BaseLibName {
			continue
		}
		if _, ok := allowed[name]; ok {
			open(name)
		}
	}

	// Drop any standard library that is NOT in the whitelist. Namespaced
	// libraries (os, io, table, ...) live in their own global; nilling the
	// global removes them entirely, even on a recycled LState.
	for name := range stdLibOpeners {
		if name == Lua.BaseLibName || name == Lua.LoadLibName {
			// Base has no namespace and Load (package) is special; both are
			// handled by inclusion above. We don't nil Base/Load globals.
			continue
		}
		if _, ok := allowed[name]; !ok {
			e.L.SetGlobal(name, Lua.LNil)
		}
	}
}

// ClearGlobals removes the named globals from the LState by setting them to
// nil. It is used by the engine to strip business globals before returning the
// LState to the pool, so recycled LStates don't leak globals from a previous
// engine instance. Built-in globals (standard library, GetLuaPath, ...) are
// untouched since they are not in the business set.
func (e *virtualMachine) ClearGlobals(names []string) {
	if e.L == nil {
		return
	}
	for _, name := range names {
		e.L.SetGlobal(name, Lua.LNil)
	}
}

// Destroy returns the borrowed Lua state to the pool.
func (e *virtualMachine) Destroy() {
	if e.L != nil {
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
