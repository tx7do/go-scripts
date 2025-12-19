package lua

import "errors"

var (
	// ErrLuaEngineNotInitialized Lua 引擎未初始化错误
	ErrLuaEngineNotInitialized = errors.New("lua engine not initialized")

	// ErrLuaEngineAlreadyInitialized Lua 引擎已初始化错误
	ErrLuaEngineAlreadyInitialized = errors.New("lua engine already initialized")

	// ErrLuaVMNotInitialized Lua 虚拟机未初始化错误
	ErrLuaVMNotInitialized = errors.New("lua VM not initialized")
)
