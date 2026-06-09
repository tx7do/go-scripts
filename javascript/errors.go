package js

import "errors"

// Sentinel errors returned by the JavaScript engine.
var (
	// ErrJavascriptEngineNotInitialized is returned when an operation is invoked
	// before Init or after Close.
	ErrJavascriptEngineNotInitialized = errors.New("javascript engine not initialized")

	// ErrJavascriptEngineAlreadyInitialized is returned when Init is called on an
	// already-initialized engine.
	ErrJavascriptEngineAlreadyInitialized = errors.New("javascript engine already initialized")

	// ErrJavascriptVMNotInitialized is returned when the underlying VM is nil.
	ErrJavascriptVMNotInitialized = errors.New("javascript VM not initialized")

	// ErrJavascriptCompileFailed is returned when goja fails to compile a script.
	ErrJavascriptCompileFailed = errors.New("javascript compile failed")

	// ErrJavascriptRuntimeNotInitialized is returned when the goja runtime is nil.
	ErrJavascriptRuntimeNotInitialized = errors.New("javascript runtime not initialized")

	// ErrJavascriptExecutionFailed is returned when running a program fails.
	ErrJavascriptExecutionFailed = errors.New("javascript execution failed")

	// ErrJavascriptNoProgramLoaded is returned when ExecuteLoaded is called but
	// no program has been loaded yet.
	ErrJavascriptNoProgramLoaded = errors.New("javascript no program loaded")
)
