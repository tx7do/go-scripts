package expr

import "errors"

// Sentinel errors returned by the Expr engine.
var (
	// ErrExprEngineNotInitialized is returned when an operation is invoked
	// before Init or after Close.
	ErrExprEngineNotInitialized = errors.New("expr engine not initialized")

	// ErrExprEngineAlreadyInitialized is returned when Init is called on an
	// already-initialized engine.
	ErrExprEngineAlreadyInitialized = errors.New("expr engine already initialized")

	// ErrExprCompileFailed is returned when compiling an expression fails.
	ErrExprCompileFailed = errors.New("expr compile failed")

	// ErrExprNoExpressionLoaded is returned when Execute is called but no
	// expression has been loaded.
	ErrExprNoExpressionLoaded = errors.New("expr no expression loaded")

	// ErrExprRunFailed is returned when evaluating an expression fails.
	ErrExprRunFailed = errors.New("expr run failed")

	// ErrExprFunctionNotFound is returned when CallFunction cannot find the
	// named function.
	ErrExprFunctionNotFound = errors.New("expr function not found")

	// ErrExprGlobalNotFound is returned when GetGlobal cannot find the named
	// variable.
	ErrExprGlobalNotFound = errors.New("expr global not found")
)
