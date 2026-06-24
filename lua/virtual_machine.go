package lua

import (
	"fmt"
	"os"
	"path/filepath"

	scriptEngine "github.com/tx7do/go-scripts"

	"github.com/tengattack/gluacrypto"
	libs "github.com/vadv/gopher-lua-libs"
	"github.com/yuin/gluamapper"
	Lua "github.com/yuin/gopher-lua"
	luar "layeh.com/gopher-luar"
)

// TableMap is a convenience alias for maps exchanged between Go and Lua.
type TableMap map[string]interface{}

// virtualMachine wraps a Lua LState and the currently-loaded function.
type virtualMachine struct {
	L *Lua.LState
	F *Lua.LFunction
}

// newVirtualMachine borrows a Lua state from the pool and initializes it.
func newVirtualMachine() *virtualMachine {
	exec := &virtualMachine{
		L: luaPool.Borrow(),
	}
	exec.init()
	return exec
}

// GetRunPath returns the absolute directory of the current executable.
func GetRunPath() string {
	path, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	return path
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
func (e *virtualMachine) initBuiltinLibs() {

	e.L.OpenLibs()

	libs.Preload(e.L)

	gluacrypto.Preload(e.L)

	//lua_debugger.Preload(e.L)

	e.RegisterFunction("GetLuaPath", func(vm *Lua.LState) int {
		// absolute path of the executable's directory
		e.L.Push(Lua.LString(GetRunPath() + "/script"))
		return 1
	})
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
