package script_engine

// Type identifies a script engine implementation.
type Type string

const (
	UnknownType Type = "unknown"

	// LuaType is the Lua 5.1 engine powered by gopher-lua.
	LuaType Type = "lua"

	// JavaScriptType is the ECMAScript 5.1+ engine powered by goja.
	JavaScriptType Type = "javascript"

	// GPythonType is the pure-Go Python 3.4 engine powered by gpython.
	GPythonType Type = "gpython"

	// YaegiType is the native Go script engine powered by Traefik Yaegi.
	YaegiType Type = "yaegi"

	// WazeroType is the WebAssembly engine powered by tetratelabs/wazero.
	WazeroType Type = "wazero"

	// CELType is the Google Common Expression Language powered by cel-go.
	CELType Type = "cel"

	// ExprType is the lightweight expression engine powered by antonmedv/expr.
	ExprType Type = "expr"

	// StarlarkType is the hermetic / safe script engine powered by google/starlark-go.
	StarlarkType Type = "starlark"

	// TclType is the 100 %-compatible Tcl engine powered by modernc.org/tcl.
	TclType Type = "tcl"
)
