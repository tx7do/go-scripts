package script_engine

import "time"

// Type identifies a script engine implementation.
type Type string

const (
	// LuaType is the Type for Lua script engines.
	LuaType Type = "lua"

	// JavaScriptType is the Type for JavaScript script engines.
	JavaScriptType Type = "javascript"

	// PythonType is the Type for Python script engines.
	PythonType Type = "python"
)

// CallResult holds the return values of a function call.
type CallResult struct {
	Values []any
	Error  error
}

// ExecuteOptions controls how a script is executed.
type ExecuteOptions struct {
	Timeout  time.Duration
	Globals  map[string]any
	MaxStack int
}
