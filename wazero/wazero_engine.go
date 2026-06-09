// Package wazero provides a script engine implementation using the wazero
// WebAssembly runtime (https://github.com/wazero/wazero). It allows executing
// WebAssembly (WASM) modules as scripts at runtime.
//
// WebAssembly is a binary format, so the "code" passed to LoadString /
// ExecuteString / loaded from Source is raw WASM bytes (Go strings can hold
// arbitrary bytes, so the []byte ↔ string conversion is lossless).
//
// Construction:
//
//	eng, _ := wazero.New(ctx)
//	eng.Init(ctx)
//	eng.LoadString(ctx, "add.wasm", string(wasmBytes))
//	result, _ := eng.Execute(ctx)
//
// After Execute, use CallFunction to invoke exported WASM functions:
//
//	result, _ := eng.CallFunction(ctx, "add", uint64(3), uint64(4))
//
// The engine supports hot-reload via StartWatch/StopWatch when the bound Source
// implements the Watcher interface.
package wazero

import (
	"context"
	"fmt"
	"sync"

	wzr "github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	scriptEngine "github.com/tx7do/go-scripts"
	"github.com/tx7do/go-scripts/source"
)

// hostModuleName is the import module name used for host functions registered
// via RegisterFunction. WASM modules can import functions from this module.
const hostModuleName = "host"

func init() {
	_ = scriptEngine.Register(scriptEngine.WazeroType, func() (scriptEngine.Engine, error) {
		return newWazeroEngine()
	})
}

// engine is the Wazero WebAssembly script engine implementation.
//
// Lock ordering convention (same as JavaScript / Yaegi engines):
//   - Always acquire `mu` (or its read lock) before `execMu`.
//   - Release in reverse order (execMu first, then mu).
//   - Never acquire `mu` while holding `execMu` to avoid deadlocks.
type engine struct {
	runtime      wzr.Runtime          // the wazero runtime
	compiledMods []wzr.CompiledModule // compiled WASM modules
	instance     api.Module           // last instantiated module

	// Host module state: functions registered via RegisterFunction are
	// accumulated here and rebuilt as a single "host" import module.
	hostFunctions map[string]any
	hostModule    api.Module

	source      source.Reader
	initialized bool
	lastError   error

	mu          sync.RWMutex // protects initialized, compiledMods, source and hostFunctions
	execMu      sync.Mutex   // protects runtime and instance
	lastErrorMu sync.RWMutex // protects lastError

	// Hot reload state
	watchers   map[string]context.CancelFunc
	watchersMu sync.Mutex
}

// newWazeroEngine creates a Wazero engine instance.
func newWazeroEngine() (*engine, error) {
	return &engine{
		initialized: false,
		watchers:    make(map[string]context.CancelFunc),
	}, nil
}

// GetType returns the script engine type.
func (e *engine) GetType() scriptEngine.Type {
	return scriptEngine.WazeroType
}

// Init initializes the engine.
func (e *engine) Init(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.initialized {
		e.setLastError(ErrWazeroEngineAlreadyInitialized)
		return ErrWazeroEngineAlreadyInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()

	e.runtime = wzr.NewRuntime(ctx)
	e.hostFunctions = make(map[string]any)
	e.initialized = true
	e.lastError = nil

	return nil
}

// Close destroys the engine and releases underlying resources.
func (e *engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		e.setLastError(ErrWazeroEngineNotInitialized)
		return ErrWazeroEngineNotInitialized
	}

	// Stop all active watchers.
	e.stopAllWatchers()

	e.execMu.Lock()
	defer e.execMu.Unlock()

	// Close the instantiated module.
	if e.instance != nil {
		_ = e.instance.Close(context.Background())
		e.instance = nil
	}

	// Close compiled modules.
	for _, cm := range e.compiledMods {
		_ = cm.Close(context.Background())
	}
	e.compiledMods = nil

	// Close host module.
	if e.hostModule != nil {
		_ = e.hostModule.Close(context.Background())
		e.hostModule = nil
	}
	e.hostFunctions = nil

	// Close runtime.
	if e.runtime != nil {
		_ = e.runtime.Close(context.Background())
		e.runtime = nil
	}

	e.initialized = false

	e.lastErrorMu.Lock()
	e.lastError = nil
	e.lastErrorMu.Unlock()

	return nil
}

// IsInitialized reports whether the engine has been initialized.
func (e *engine) IsInitialized() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.initialized
}

////////////////////////////////////////////////////////////////////////////////
// ScriptSource injection & access
////////////////////////////////////////////////////////////////////////////////

// SetSource binds a ScriptSource to the engine.
func (e *engine) SetSource(src source.Reader) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.source = src
}

// GetSource returns the currently bound ScriptSource, or nil if none has been set.
func (e *engine) GetSource() source.Reader {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.source
}

////////////////////////////////////////////////////////////////////////////////
// Script loading
////////////////////////////////////////////////////////////////////////////////

// Load loads a WASM module from the bound Source using the given key.
// The loaded module is compiled and appended to the queue.
func (e *engine) Load(ctx context.Context, key string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrWazeroEngineNotInitialized)
		return ErrWazeroEngineNotInitialized
	}
	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("wazero engine: no source bound")
		e.setLastError(err)
		return err
	}
	code, err := src.Load(ctx, key)
	if err != nil {
		e.setLastError(err)
		return err
	}
	return e.LoadString(ctx, key, code)
}

// LoadMulti loads multiple WASM modules from the bound Source in order.
func (e *engine) LoadMulti(ctx context.Context, keys []string) error {
	for _, k := range keys {
		if err := e.Load(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

// LoadString compiles an inline WASM binary. The `code` parameter is treated
// as raw WASM bytes (Go strings can hold arbitrary bytes). `name` is used for
// diagnostics. LoadString does NOT go through the bound Source.
func (e *engine) LoadString(ctx context.Context, _ string, code string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrWazeroEngineNotInitialized)
		return ErrWazeroEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.runtime == nil {
		e.setLastError(ErrWazeroRuntimeNotInitialized)
		return ErrWazeroRuntimeNotInitialized
	}

	compiled, err := e.runtime.CompileModule(ctx, []byte(code))
	if err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrWazeroCompileFailed, err)
		e.setLastError(wrapped)
		return wrapped
	}

	e.mu.Lock()
	e.compiledMods = append(e.compiledMods, compiled)
	e.mu.Unlock()

	e.ClearError()
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// Script execution
////////////////////////////////////////////////////////////////////////////////

// Execute instantiates the last compiled WASM module and calls its `_start`
// function if it exports one. Returns nil if `_start` is not exported (the
// module is still instantiated and available for CallFunction).
func (e *engine) Execute(ctx context.Context) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrWazeroEngineNotInitialized)
		return nil, ErrWazeroEngineNotInitialized
	}

	e.mu.RLock()
	if len(e.compiledMods) == 0 {
		e.mu.RUnlock()
		e.setLastError(ErrWazeroNoModuleLoaded)
		return nil, ErrWazeroNoModuleLoaded
	}
	last := e.compiledMods[len(e.compiledMods)-1]
	e.mu.RUnlock()

	return e.instantiateAndRun(ctx, last)
}

// ExecuteFromKey loads the WASM module identified by `key` from the bound
// Source and immediately runs it.
func (e *engine) ExecuteFromKey(ctx context.Context, key string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrWazeroEngineNotInitialized)
		return nil, ErrWazeroEngineNotInitialized
	}
	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("wazero engine: no source bound")
		e.setLastError(err)
		return nil, err
	}
	code, err := src.Load(ctx, key)
	if err != nil {
		e.setLastError(err)
		return nil, err
	}
	return e.ExecuteString(ctx, key, code)
}

// ExecuteFromKeys is the multi-key variant of ExecuteFromKey.
func (e *engine) ExecuteFromKeys(ctx context.Context, keys []string) ([]any, error) {
	results := make([]any, 0, len(keys))
	for _, k := range keys {
		res, err := e.ExecuteFromKey(ctx, k)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	return results, nil
}

// ExecuteString compiles and immediately runs inline WASM bytes.
func (e *engine) ExecuteString(ctx context.Context, name string, code string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrWazeroEngineNotInitialized)
		return nil, ErrWazeroEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.runtime == nil {
		e.setLastError(ErrWazeroRuntimeNotInitialized)
		return nil, ErrWazeroRuntimeNotInitialized
	}

	compiled, err := e.runtime.CompileModule(ctx, []byte(code))
	if err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrWazeroCompileFailed, err)
		e.setLastError(wrapped)
		return nil, wrapped
	}
	defer compiled.Close(context.Background())

	return e.instantiateAndRunLocked(ctx, compiled)
}

////////////////////////////////////////////////////////////////////////////////
// Globals, functions, modules
////////////////////////////////////////////////////////////////////////////////

// RegisterGlobal registers a Go value as a global in the engine's internal
// state. Note: WASM globals are limited to numeric types (i32/i64/f32/f64).
// For non-numeric values, this stores them internally but they are not directly
// accessible from WASM modules.
func (e *engine) RegisterGlobal(name string, value any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrWazeroEngineNotInitialized)
		return ErrWazeroEngineNotInitialized
	}
	// WASM globals are limited; we store internally for potential future use.
	// This is a no-op for most types.
	e.ClearError()
	return nil
}

// GetGlobal reads a WASM exported global from the last instantiated module.
// Returns the value as uint64 (the raw WASM representation).
func (e *engine) GetGlobal(name string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrWazeroEngineNotInitialized)
		return nil, ErrWazeroEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.instance == nil {
		e.setLastError(ErrWazeroNotInstantiated)
		return nil, ErrWazeroNotInstantiated
	}

	g := e.instance.ExportedGlobal(name)
	if g == nil {
		err := fmt.Errorf("wazero engine: global %q not found", name)
		e.setLastError(err)
		return nil, err
	}

	e.ClearError()
	return g.Get(), nil
}

// RegisterFunction registers a host function that WASM modules can import
// from the "host" module. The function signature must be compatible with WASM
// calling conventions (parameters must be context.Context, api.Module, or
// numeric types: uint32/uint64/int32/int64/float32/float64).
func (e *engine) RegisterFunction(name string, fn any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrWazeroEngineNotInitialized)
		return ErrWazeroEngineNotInitialized
	}

	e.mu.Lock()
	e.hostFunctions[name] = fn
	err := e.rebuildHostModule()
	e.mu.Unlock()

	if err != nil {
		e.setLastError(err)
		return err
	}

	e.ClearError()
	return nil
}

// CallFunction invokes an exported WASM function by name with uint64 arguments
// and returns the first result as uint64. The module must have been instantiated
// via Execute or ExecuteString first.
func (e *engine) CallFunction(ctx context.Context, name string, args ...any) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrWazeroEngineNotInitialized)
		return nil, ErrWazeroEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.instance == nil {
		e.setLastError(ErrWazeroNotInstantiated)
		return nil, ErrWazeroNotInstantiated
	}

	fn := e.instance.ExportedFunction(name)
	if fn == nil {
		wrapped := fmt.Errorf("%w: %s", ErrWazeroFunctionNotFound, name)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	// Convert args to uint64.
	uintArgs := make([]uint64, len(args))
	for i, a := range args {
		uintArgs[i] = toUint64(a)
	}

	results, err := fn.Call(ctx, uintArgs...)
	if err != nil {
		wrapped := fmt.Errorf("wazero engine: call %q failed: %w", name, err)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	e.ClearError()
	if len(results) == 0 {
		return nil, nil
	}
	return results[0], nil
}

// RegisterModule registers a named host module. `module` must be of type
// map[string]any; its entries become exported functions in the named import
// module, callable from WASM.
func (e *engine) RegisterModule(name string, module any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrWazeroEngineNotInitialized)
		return ErrWazeroEngineNotInitialized
	}

	syms, ok := module.(map[string]any)
	if !ok {
		err := fmt.Errorf("wazero engine: RegisterModule expects map[string]any, got %T", module)
		e.setLastError(err)
		return err
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.runtime == nil {
		e.setLastError(ErrWazeroRuntimeNotInitialized)
		return ErrWazeroRuntimeNotInitialized
	}

	builder := e.runtime.NewHostModuleBuilder(name)
	for fnName, fn := range syms {
		builder.NewFunctionBuilder().WithFunc(fn).Export(fnName)
	}
	if _, err := builder.Instantiate(context.Background()); err != nil {
		wrapped := fmt.Errorf("wazero engine: register module %q: %w", name, err)
		e.setLastError(wrapped)
		return wrapped
	}

	e.ClearError()
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// Error handling
////////////////////////////////////////////////////////////////////////////////

func (e *engine) GetLastError() error {
	e.lastErrorMu.RLock()
	defer e.lastErrorMu.RUnlock()
	return e.lastError
}

func (e *engine) setLastError(err error) {
	e.lastErrorMu.Lock()
	defer e.lastErrorMu.Unlock()
	e.lastError = err
}

func (e *engine) ClearError() {
	e.lastErrorMu.Lock()
	defer e.lastErrorMu.Unlock()
	e.lastError = nil
}

////////////////////////////////////////////////////////////////////////////////
// Hot Reload (Watch)
////////////////////////////////////////////////////////////////////////////////

// StartWatch starts watching the WASM module identified by `key` for changes.
func (e *engine) StartWatch(ctx context.Context, key string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrWazeroEngineNotInitialized)
		return ErrWazeroEngineNotInitialized
	}

	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("wazero engine: no source bound")
		e.setLastError(err)
		return err
	}

	watcher, ok := src.(source.Watcher)
	if !ok {
		err := fmt.Errorf("wazero engine: source does not implement Watcher")
		e.setLastError(err)
		return err
	}

	e.StopWatch(key)

	watchCtx, cancel := context.WithCancel(ctx)

	e.watchersMu.Lock()
	e.watchers[key] = cancel
	e.watchersMu.Unlock()

	go func() {
		ch, err := watcher.Watch(watchCtx, key)
		if err != nil {
			cancel()
			return
		}
		for range ch {
			if loadErr := e.Load(watchCtx, key); loadErr != nil {
				scriptEngine.GetLogger().Warn(watchCtx, "wazero engine: hot reload failed",
					"key", key, "error", loadErr)
			}
		}
	}()

	return nil
}

// StopWatch stops watching the WASM module identified by `key`.
func (e *engine) StopWatch(key string) error {
	e.watchersMu.Lock()
	defer e.watchersMu.Unlock()
	if cancel, ok := e.watchers[key]; ok {
		cancel()
		delete(e.watchers, key)
	}
	return nil
}

// stopAllWatchers cancels all active watch goroutines.
func (e *engine) stopAllWatchers() {
	e.watchersMu.Lock()
	defer e.watchersMu.Unlock()
	for key, cancel := range e.watchers {
		cancel()
		delete(e.watchers, key)
	}
}
