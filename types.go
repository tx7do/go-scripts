package script_engine

import "time"

type Type string

const (
	// LuaType Lua 脚本引擎类型
	LuaType Type = "lua"

	// JavaScriptType JavaScript 脚本引擎类型
	JavaScriptType Type = "javascript"
)

// CallResult 函数调用结果
type CallResult struct {
	Values []any
	Error  error
}

// ExecuteOptions 执行选项
type ExecuteOptions struct {
	Timeout  time.Duration
	Globals  map[string]any
	MaxStack int
}
