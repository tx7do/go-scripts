package lua

import "errors"

// Sentinel errors returned by the Lua engine.
var (
	// ErrLuaEngineNotInitialized is returned when an operation is invoked before
	// Init or after Close.
	ErrLuaEngineNotInitialized = errors.New("lua engine not initialized")

	// ErrLuaEngineAlreadyInitialized is returned when Init is called on an
	// already-initialized engine.
	ErrLuaEngineAlreadyInitialized = errors.New("lua engine already initialized")

	// ErrLuaVMNotInitialized is returned when the underlying Lua VM is nil.
	ErrLuaVMNotInitialized = errors.New("lua VM not initialized")
)
