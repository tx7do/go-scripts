package gpython

import "errors"

// Sentinel errors returned by the GPython engine.
var (
	// ErrGPythonEngineNotInitialized is returned when an operation is invoked
	// before Init or after Close.
	ErrGPythonEngineNotInitialized = errors.New("gpython engine not initialized")

	// ErrGPythonEngineAlreadyInitialized is returned when Init is called on an
	// already-initialized engine.
	ErrGPythonEngineAlreadyInitialized = errors.New("gpython engine already initialized")

	// ErrGPythonContextNotInitialized is returned when the underlying py.Context
	// is nil.
	ErrGPythonContextNotInitialized = errors.New("gpython context not initialized")

	// ErrGPythonCompileFailed is returned when compiling Python source fails.
	ErrGPythonCompileFailed = errors.New("gpython compile failed")

	// ErrGPythonNoScriptLoaded is returned when Execute is called but no script
	// has been loaded.
	ErrGPythonNoScriptLoaded = errors.New("gpython no script loaded")

	// ErrGPythonRunFailed is returned when running Python code fails.
	ErrGPythonRunFailed = errors.New("gpython run failed")

	// ErrGPythonFunctionNotFound is returned when CallFunction cannot find the
	// named function.
	ErrGPythonFunctionNotFound = errors.New("gpython function not found")

	// ErrGPythonGlobalNotFound is returned when GetGlobal cannot find the named
	// variable.
	ErrGPythonGlobalNotFound = errors.New("gpython global not found")
)
