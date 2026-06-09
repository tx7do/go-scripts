package wazero

import "errors"

// Sentinel errors returned by the Wazero engine.
var (
	// ErrWazeroEngineNotInitialized is returned when an operation is invoked
	// before Init or after Close.
	ErrWazeroEngineNotInitialized = errors.New("wazero engine not initialized")

	// ErrWazeroEngineAlreadyInitialized is returned when Init is called on an
	// already-initialized engine.
	ErrWazeroEngineAlreadyInitialized = errors.New("wazero engine already initialized")

	// ErrWazeroRuntimeNotInitialized is returned when the underlying wazero
	// runtime is nil.
	ErrWazeroRuntimeNotInitialized = errors.New("wazero runtime not initialized")

	// ErrWazeroCompileFailed is returned when compiling WASM bytes fails.
	ErrWazeroCompileFailed = errors.New("wazero compile failed")

	// ErrWazeroInstantiateFailed is returned when instantiating a WASM module fails.
	ErrWazeroInstantiateFailed = errors.New("wazero instantiate failed")

	// ErrWazeroNoModuleLoaded is returned when Execute is called but no module
	// has been compiled yet.
	ErrWazeroNoModuleLoaded = errors.New("wazero no module loaded")

	// ErrWazeroFunctionNotFound is returned when CallFunction cannot find the
	// named exported function in the instantiated module.
	ErrWazeroFunctionNotFound = errors.New("wazero function not found")

	// ErrWazeroNotInstantiated is returned when CallFunction / GetGlobal is
	// called before any module has been instantiated via Execute.
	ErrWazeroNotInstantiated = errors.New("wazero module not instantiated")
)
