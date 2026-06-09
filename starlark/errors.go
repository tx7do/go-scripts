package starlark

import "errors"

// Sentinel errors returned by the Starlark engine.
var (
	// ErrStarlarkEngineNotInitialized is returned when an operation is invoked
	// before Init or after Close.
	ErrStarlarkEngineNotInitialized = errors.New("starlark engine not initialized")

	// ErrStarlarkEngineAlreadyInitialized is returned when Init is called on an
	// already-initialized engine.
	ErrStarlarkEngineAlreadyInitialized = errors.New("starlark engine already initialized")

	// ErrStarlarkNoScriptLoaded is returned when Execute is called but no script
	// has been loaded.
	ErrStarlarkNoScriptLoaded = errors.New("starlark no script loaded")

	// ErrStarlarkExecFailed is returned when executing a Starlark script fails.
	ErrStarlarkExecFailed = errors.New("starlark exec failed")

	// ErrStarlarkFunctionNotFound is returned when CallFunction cannot find the
	// named function.
	ErrStarlarkFunctionNotFound = errors.New("starlark function not found")

	// ErrStarlarkGlobalNotFound is returned when GetGlobal cannot find the named
	// variable.
	ErrStarlarkGlobalNotFound = errors.New("starlark global not found")

	// ErrStarlarkCallFailed is returned when calling a Starlark function fails.
	ErrStarlarkCallFailed = errors.New("starlark call failed")
)
