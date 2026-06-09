package cel

import "errors"

// Sentinel errors returned by the CEL engine.
var (
	// ErrCELEngineNotInitialized is returned when an operation is invoked
	// before Init or after Close.
	ErrCELEngineNotInitialized = errors.New("cel engine not initialized")

	// ErrCELEngineAlreadyInitialized is returned when Init is called on an
	// already-initialized engine.
	ErrCELEngineAlreadyInitialized = errors.New("cel engine already initialized")

	// ErrCLEEnvNotInitialized is returned when the underlying cel-go Env is nil.
	ErrCLEEnvNotInitialized = errors.New("cel env not initialized")

	// ErrCELCompileFailed is returned when compiling a CEL expression fails.
	ErrCELCompileFailed = errors.New("cel compile failed")

	// ErrCELNoExpressionLoaded is returned when Execute is called but no
	// expression has been loaded.
	ErrCELNoExpressionLoaded = errors.New("cel no expression loaded")

	// ErrCELEvalFailed is returned when evaluating a CEL expression fails.
	ErrCELEvalFailed = errors.New("cel eval failed")

	// ErrCELFunctionNotFound is returned when CallFunction cannot find the
	// named function.
	ErrCELFunctionNotFound = errors.New("cel function not found")

	// ErrCELGlobalNotFound is returned when GetGlobal cannot find the named
	// variable.
	ErrCELGlobalNotFound = errors.New("cel global not found")
)
