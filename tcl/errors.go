package tcl

import "errors"

// Sentinel errors returned by the TCL engine.
var (
	// ErrTCLEngineNotInitialized is returned when an operation is invoked
	// before Init or after Close.
	ErrTCLEngineNotInitialized = errors.New("tcl engine not initialized")

	// ErrTCLEngineAlreadyInitialized is returned when Init is called on an
	// already-initialized engine.
	ErrTCLEngineAlreadyInitialized = errors.New("tcl engine already initialized")

	// ErrTCLEvalFailed is returned when evaluating a TCL script fails.
	ErrTCLEvalFailed = errors.New("tcl eval failed")

	// ErrTCLNoScriptLoaded is returned when Execute is called but no script
	// has been loaded.
	ErrTCLNoScriptLoaded = errors.New("tcl no script loaded")

	// ErrTCLFunctionNotFound is returned when CallFunction cannot find the
	// named function.
	ErrTCLFunctionNotFound = errors.New("tcl function not found")

	// ErrTCLGlobalNotFound is returned when GetGlobal cannot find the named
	// variable.
	ErrTCLGlobalNotFound = errors.New("tcl global not found")

	// ErrTCLCallFailed is returned when calling a TCL function fails.
	ErrTCLCallFailed = errors.New("tcl call failed")
)
