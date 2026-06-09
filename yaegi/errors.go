package yaegi

import "errors"

// Sentinel errors returned by the Yaegi engine.
var (
	// ErrYaegiEngineNotInitialized is returned when an operation is invoked
	// before Init or after Close.
	ErrYaegiEngineNotInitialized = errors.New("yaegi engine not initialized")

	// ErrYaegiEngineAlreadyInitialized is returned when Init is called on an
	// already-initialized engine.
	ErrYaegiEngineAlreadyInitialized = errors.New("yaegi engine already initialized")

	// ErrYaegiInterpreterNotInitialized is returned when the underlying yaegi
	// interpreter is nil.
	ErrYaegiInterpreterNotInitialized = errors.New("yaegi interpreter not initialized")

	// ErrYaegiEvalFailed is returned when evaluating a script fails.
	ErrYaegiEvalFailed = errors.New("yaegi eval failed")

	// ErrYaegiNoScriptLoaded is returned when Execute is called but no script
	// has been loaded yet.
	ErrYaegiNoScriptLoaded = errors.New("yaegi no script loaded")

	// ErrYaegiFunctionNotFound is returned when CallFunction cannot find the
	// named function in the interpreter.
	ErrYaegiFunctionNotFound = errors.New("yaegi function not found")
)
