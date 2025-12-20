package js

import "errors"

var (
	// ErrJavascriptEngineNotInitialized JavaScript 引擎未初始化错误
	ErrJavascriptEngineNotInitialized = errors.New("javascript engine not initialized")

	// ErrJavascriptEngineAlreadyInitialized JavaScript 引擎已初始化错误
	ErrJavascriptEngineAlreadyInitialized = errors.New("javascript engine already initialized")

	// ErrJavascriptVMNotInitialized JavaScript 虚拟机未初始化错误
	ErrJavascriptVMNotInitialized = errors.New("javascript VM not initialized")

	ErrJavascriptCompileFailed = errors.New("javascript compile failed")

	ErrJavascriptRuntimeNotInitialized = errors.New("javascript runtime not initialized")

	ErrJavascriptExecutionFailed = errors.New("javascript execution failed")

	ErrJavascriptNoProgramLoaded = errors.New("javascript no program loaded")
)
