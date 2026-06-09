// Package starlark provides a script engine implementation using Google's
// Starlark-Go (https://github.com/google/starlark-go). Starlark is a dialect
// of Python intended for embedded scripting in applications.
//
// Construction:
//
//	eng, _ := starlark.New(ctx)
//	eng.Init(ctx)
//	eng.RegisterGlobal("name", "world")
//	eng.LoadString(ctx, "hello", `greeting = "Hello, " + name`)
//	eng.Execute(ctx)
//
// The engine supports hot-reload via StartWatch/StopWatch when the bound Source
// implements the Watcher interface.
package starlark

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"go.starlark.net/starlark"

	scriptEngine "github.com/tx7do/go-scripts"
	"github.com/tx7do/go-scripts/source"
)

func init() {
	_ = scriptEngine.Register(scriptEngine.StarlarkType, func() (scriptEngine.Engine, error) {
		return newStarlarkEngine()
	})
}

// scriptEntry holds source code and its diagnostic name.
type scriptEntry struct {
	name string
	src  string
}

// engine is the Starlark script engine implementation.
//
// Lock ordering convention (same as other engines):
//   - Always acquire `mu` (or its read lock) before `execMu`.
//   - Release in reverse order (execMu first, then mu).
//   - Never acquire `mu` while holding `execMu` to avoid deadlocks.
type engine struct {
	// hostPredeclared stores Go values/functions registered via
	// RegisterGlobal / RegisterFunction / RegisterModule.
	hostPredeclared starlark.StringDict

	// scriptGlobals accumulates globals defined by executed scripts.
	scriptGlobals starlark.StringDict

	// scripts holds loaded sources waiting to be executed.
	scripts []scriptEntry

	// hostFuncs stores original Go functions for CallFunction host-path.
	hostFuncs map[string]any

	source      source.Reader
	initialized bool
	lastError   error

	mu          sync.RWMutex // protects initialized, scripts, source, hostFuncs, hostPredeclared
	execMu      sync.Mutex   // protects scriptGlobals, thread execution
	lastErrorMu sync.RWMutex // protects lastError

	// Hot reload state
	watchers   map[string]context.CancelFunc
	watchersMu sync.Mutex
}

// newStarlarkEngine creates a Starlark engine instance.
func newStarlarkEngine() (*engine, error) {
	return &engine{
		initialized:     false,
		watchers:        make(map[string]context.CancelFunc),
		hostPredeclared: make(starlark.StringDict),
		scriptGlobals:   make(starlark.StringDict),
		hostFuncs:       make(map[string]any),
	}, nil
}

// GetType returns the script engine type.
func (e *engine) GetType() scriptEngine.Type {
	return scriptEngine.StarlarkType
}

// Init initializes the engine.
func (e *engine) Init(_ context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.initialized {
		e.setLastError(ErrStarlarkEngineAlreadyInitialized)
		return ErrStarlarkEngineAlreadyInitialized
	}

	e.hostPredeclared = make(starlark.StringDict)
	e.scriptGlobals = make(starlark.StringDict)
	e.hostFuncs = make(map[string]any)
	e.scripts = nil
	e.initialized = true
	e.lastError = nil

	return nil
}

// Close destroys the engine and releases resources.
func (e *engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		e.setLastError(ErrStarlarkEngineNotInitialized)
		return ErrStarlarkEngineNotInitialized
	}

	e.stopAllWatchers()

	e.execMu.Lock()
	e.hostPredeclared = nil
	e.scriptGlobals = nil
	e.scripts = nil
	e.hostFuncs = nil
	e.initialized = false
	e.execMu.Unlock()

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

// GetSource returns the currently bound ScriptSource, or nil.
func (e *engine) GetSource() source.Reader {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.source
}

////////////////////////////////////////////////////////////////////////////////
// Script loading
////////////////////////////////////////////////////////////////////////////////

// Load loads a Starlark script from the bound Source.
func (e *engine) Load(ctx context.Context, key string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrStarlarkEngineNotInitialized)
		return ErrStarlarkEngineNotInitialized
	}
	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("starlark engine: no source bound")
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

// LoadMulti loads multiple scripts from the bound Source in order.
func (e *engine) LoadMulti(ctx context.Context, keys []string) error {
	for _, k := range keys {
		if err := e.Load(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

// LoadString stores an inline Starlark source string for later execution.
func (e *engine) LoadString(_ context.Context, name string, code string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrStarlarkEngineNotInitialized)
		return ErrStarlarkEngineNotInitialized
	}

	e.mu.Lock()
	e.scripts = append(e.scripts, scriptEntry{name: name, src: code})
	e.mu.Unlock()

	e.ClearError()
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// Script execution
////////////////////////////////////////////////////////////////////////////////

// Execute runs all previously loaded Starlark scripts in order. All scripts
// share the same globals, so variables/functions defined in one are visible
// to subsequent ones.
func (e *engine) Execute(_ context.Context) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrStarlarkEngineNotInitialized)
		return nil, ErrStarlarkEngineNotInitialized
	}

	e.mu.RLock()
	if len(e.scripts) == 0 {
		e.mu.RUnlock()
		e.setLastError(ErrStarlarkNoScriptLoaded)
		return nil, ErrStarlarkNoScriptLoaded
	}
	scripts := make([]scriptEntry, len(e.scripts))
	copy(scripts, e.scripts)
	hostPredeclared := make(starlark.StringDict, len(e.hostPredeclared))
	for k, v := range e.hostPredeclared {
		hostPredeclared[k] = v
	}
	e.mu.RUnlock()

	e.execMu.Lock()
	defer e.execMu.Unlock()

	for _, s := range scripts {
		// Merge host predeclared + existing script globals as predeclared.
		predeclared := make(starlark.StringDict)
		for k, v := range hostPredeclared {
			predeclared[k] = v
		}
		for k, v := range e.scriptGlobals {
			predeclared[k] = v
		}

		thread := &starlark.Thread{Name: s.name}
		globals, err := starlark.ExecFile(thread, s.name, s.src, predeclared)
		if err != nil {
			wrapped := fmt.Errorf("%w: %s: %v", ErrStarlarkExecFailed, s.name, err)
			e.setLastError(wrapped)
			return nil, wrapped
		}

		// Accumulate globals.
		for k, v := range globals {
			e.scriptGlobals[k] = v
		}
	}

	e.ClearError()
	return nil, nil
}

// ExecuteFromKey loads and immediately runs a script from the bound Source.
func (e *engine) ExecuteFromKey(ctx context.Context, key string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrStarlarkEngineNotInitialized)
		return nil, ErrStarlarkEngineNotInitialized
	}
	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("starlark engine: no source bound")
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

// ExecuteFromKeys is the multi-key variant.
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

// ExecuteString compiles and immediately runs an inline Starlark source string.
func (e *engine) ExecuteString(_ context.Context, name string, code string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrStarlarkEngineNotInitialized)
		return nil, ErrStarlarkEngineNotInitialized
	}

	e.mu.RLock()
	hostPredeclared := make(starlark.StringDict, len(e.hostPredeclared))
	for k, v := range e.hostPredeclared {
		hostPredeclared[k] = v
	}
	e.mu.RUnlock()

	e.execMu.Lock()
	defer e.execMu.Unlock()

	// Merge host predeclared + existing script globals.
	predeclared := make(starlark.StringDict)
	for k, v := range hostPredeclared {
		predeclared[k] = v
	}
	for k, v := range e.scriptGlobals {
		predeclared[k] = v
	}

	thread := &starlark.Thread{Name: name}
	globals, err := starlark.ExecFile(thread, name, code, predeclared)
	if err != nil {
		wrapped := fmt.Errorf("%w: %s: %v", ErrStarlarkExecFailed, name, err)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	for k, v := range globals {
		e.scriptGlobals[k] = v
	}

	e.ClearError()
	return nil, nil
}

////////////////////////////////////////////////////////////////////////////////
// Global Variable Registration
////////////////////////////////////////////////////////////////////////////////

// RegisterGlobal registers or overwrites a named variable visible to scripts.
func (e *engine) RegisterGlobal(name string, value any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrStarlarkEngineNotInitialized)
		return ErrStarlarkEngineNotInitialized
	}

	val := goToStarlark(value)

	e.mu.Lock()
	e.hostPredeclared[name] = val
	e.mu.Unlock()

	e.ClearError()
	return nil
}

// GetGlobal reads the value of a global variable.
func (e *engine) GetGlobal(name string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrStarlarkEngineNotInitialized)
		return nil, ErrStarlarkEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()

	// Check script globals first, then host predeclared.
	if v, ok := e.scriptGlobals[name]; ok {
		e.ClearError()
		return starlarkToGo(v), nil
	}

	e.mu.RLock()
	v, ok := e.hostPredeclared[name]
	e.mu.RUnlock()
	if !ok {
		wrapped := fmt.Errorf("%w: %s", ErrStarlarkGlobalNotFound, name)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	e.ClearError()
	return starlarkToGo(v), nil
}

////////////////////////////////////////////////////////////////////////////////
// Function Call
////////////////////////////////////////////////////////////////////////////////

// RegisterFunction registers a Go function that scripts can call by name.
func (e *engine) RegisterFunction(name string, fn any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrStarlarkEngineNotInitialized)
		return ErrStarlarkEngineNotInitialized
	}

	if reflect.TypeOf(fn).Kind() != reflect.Func {
		err := fmt.Errorf("starlark engine: RegisterFunction expects a func type, got %T", fn)
		e.setLastError(err)
		return err
	}

	e.mu.Lock()
	e.hostFuncs[name] = fn
	builtin := starlark.NewBuiltin(name, func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		return callHostFuncFromStarlark(fn, args)
	})
	e.hostPredeclared[name] = builtin
	e.mu.Unlock()

	e.ClearError()
	return nil
}

// CallFunction invokes a function by name.
func (e *engine) CallFunction(_ context.Context, name string, args ...any) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrStarlarkEngineNotInitialized)
		return nil, ErrStarlarkEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()

	// Look in script globals first.
	fn, ok := e.scriptGlobals[name]
	if !ok {
		e.mu.RLock()
		fn, ok = e.hostPredeclared[name]
		e.mu.RUnlock()
	}
	if !ok || fn == nil {
		wrapped := fmt.Errorf("%w: %s", ErrStarlarkFunctionNotFound, name)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	// If it's a host function stored internally, call via Go reflection.
	e.mu.RLock()
	hostFn, isHost := e.hostFuncs[name]
	e.mu.RUnlock()
	if isHost {
		result := callHostFunc(hostFn, args)
		e.ClearError()
		return result, nil
	}

	// Check if callable.
	callable, callOk := fn.(starlark.Callable)
	if !callOk {
		wrapped := fmt.Errorf("%w: %s is not callable", ErrStarlarkCallFailed, name)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	// Build starlark args.
	slArgs := make(starlark.Tuple, len(args))
	for i, a := range args {
		slArgs[i] = goToStarlark(a)
	}

	thread := &starlark.Thread{Name: "call_" + name}
	result, err := starlark.Call(thread, callable, slArgs, nil)
	if err != nil {
		wrapped := fmt.Errorf("%w: %s: %v", ErrStarlarkCallFailed, name, err)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	e.ClearError()
	return starlarkToGo(result), nil
}

////////////////////////////////////////////////////////////////////////////////
// Module Management
////////////////////////////////////////////////////////////////////////////////

// RegisterModule registers a module from a map of values. Each key becomes a
// global variable prefixed with the module name (like gpython).
func (e *engine) RegisterModule(name string, module any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrStarlarkEngineNotInitialized)
		return ErrStarlarkEngineNotInitialized
	}

	syms, ok := module.(map[string]any)
	if !ok {
		err := fmt.Errorf("starlark engine: RegisterModule expects map[string]any, got %T", module)
		e.setLastError(err)
		return err
	}

	e.mu.Lock()
	for key, val := range syms {
		globalName := name + "_" + key
		e.hostPredeclared[globalName] = goToStarlark(val)
	}
	e.mu.Unlock()

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

// StartWatch starts watching the script identified by key for changes.
func (e *engine) StartWatch(ctx context.Context, key string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrStarlarkEngineNotInitialized)
		return ErrStarlarkEngineNotInitialized
	}

	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("starlark engine: no source bound")
		e.setLastError(err)
		return err
	}

	watcher, ok := src.(source.Watcher)
	if !ok {
		err := fmt.Errorf("starlark engine: source does not implement Watcher")
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
				scriptEngine.GetLogger().Warn(watchCtx, "starlark engine: hot reload failed",
					"key", key, "error", loadErr)
			}
		}
	}()

	return nil
}

// StopWatch stops watching the script identified by key.
func (e *engine) StopWatch(key string) error {
	e.watchersMu.Lock()
	defer e.watchersMu.Unlock()
	if cancel, ok := e.watchers[key]; ok {
		cancel()
		delete(e.watchers, key)
	}
	return nil
}

func (e *engine) stopAllWatchers() {
	e.watchersMu.Lock()
	defer e.watchersMu.Unlock()
	for key, cancel := range e.watchers {
		cancel()
		delete(e.watchers, key)
	}
}
