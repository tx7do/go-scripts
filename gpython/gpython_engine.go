// Package gpython provides a script engine implementation using the gpython
// Python 3 interpreter (https://github.com/go-python/gpython). It allows
// executing Python scripts at runtime in a pure Go environment without CPython
// dependencies.
//
// Construction:
//
//	eng, _ := gpython.New(ctx)
//	eng.Init(ctx)
//	eng.LoadString(ctx, "hello", `print("hello world")`)
//	result, err := eng.Execute(ctx)
//
// The engine supports hot-reload via StartWatch/StopWatch when the bound Source
// implements the Watcher interface.
package gpython

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	// Essential: registers native built-in modules (sys, time, math, etc.)
	_ "github.com/go-python/gpython/stdlib"

	"github.com/go-python/gpython/py"

	scriptEngine "github.com/tx7do/go-scripts"
	"github.com/tx7do/go-scripts/source"
)

func init() {
	_ = scriptEngine.Register(scriptEngine.GPythonType, func() (scriptEngine.Engine, error) {
		return newGPythonEngine()
	})
}

// scriptEntry holds compiled Python code and its source.
type scriptEntry struct {
	name string
	code *py.Code
}

// engine is the GPython script engine implementation.
//
// Lock ordering convention (same as other engines):
//   - Always acquire `mu` (or its read lock) before `execMu`.
//   - Release in reverse order (execMu first, then mu).
//   - Never acquire `mu` while holding `execMu` to avoid deadlocks.
type engine struct {
	ctx     py.Context    // the gpython context (interpreter instance)
	module  *py.Module    // the main module for script execution
	scripts []scriptEntry // compiled scripts queued for execution

	// hostFuncs stores Go functions registered via RegisterFunction.
	// They are injected into the module globals as Python functions.
	hostFuncs map[string]any

	source      source.Reader
	initialized bool
	lastError   error

	mu          sync.RWMutex // protects initialized, scripts, source, hostFuncs
	execMu      sync.Mutex   // protects ctx, module
	lastErrorMu sync.RWMutex // protects lastError

	// Hot reload state
	watchers   map[string]context.CancelFunc
	watchersMu sync.Mutex
}

// newGPythonEngine creates a GPython engine instance.
func newGPythonEngine() (*engine, error) {
	return &engine{
		initialized: false,
		watchers:    make(map[string]context.CancelFunc),
	}, nil
}

// GetType returns the script engine type.
func (e *engine) GetType() scriptEngine.Type {
	return scriptEngine.GPythonType
}

// Init initializes the engine by creating a fresh gpython context and module.
func (e *engine) Init(_ context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.initialized {
		e.setLastError(ErrGPythonEngineAlreadyInitialized)
		return ErrGPythonEngineAlreadyInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()

	e.ctx = py.NewContext(py.DefaultContextOpts())

	mainImpl := py.ModuleImpl{
		CodeSrc: "",
	}

	mod, err := e.ctx.ModuleInit(&mainImpl)
	if err != nil {
		wrapped := fmt.Errorf("gpython engine: init module: %w", err)
		e.setLastError(wrapped)
		return wrapped
	}

	e.module = mod
	e.hostFuncs = make(map[string]any)
	e.scripts = nil
	e.initialized = true
	e.lastError = nil

	return nil
}

// Close destroys the engine and releases underlying resources.
func (e *engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		e.setLastError(ErrGPythonEngineNotInitialized)
		return ErrGPythonEngineNotInitialized
	}

	e.stopAllWatchers()

	e.execMu.Lock()
	defer e.execMu.Unlock()

	if e.ctx != nil {
		e.ctx.Close()
		e.ctx = nil
	}
	e.module = nil
	e.scripts = nil
	e.hostFuncs = nil
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

// Load loads a Python script from the bound Source using the given key.
func (e *engine) Load(ctx context.Context, key string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrGPythonEngineNotInitialized)
		return ErrGPythonEngineNotInitialized
	}
	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("gpython engine: no source bound")
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

// LoadMulti loads multiple Python scripts from the bound Source in order.
func (e *engine) LoadMulti(ctx context.Context, keys []string) error {
	for _, k := range keys {
		if err := e.Load(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

// LoadString compiles an inline Python source string. `name` is used for
// diagnostics. The compiled code is appended to the queue consumed by Execute.
func (e *engine) LoadString(_ context.Context, name string, code string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrGPythonEngineNotInitialized)
		return ErrGPythonEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.ctx == nil {
		e.setLastError(ErrGPythonContextNotInitialized)
		return ErrGPythonContextNotInitialized
	}

	compiled, err := py.Compile(code, name, py.ExecMode, 0, true)
	if err != nil {
		wrapped := fmt.Errorf("%w: %s: %v", ErrGPythonCompileFailed, name, err)
		e.setLastError(wrapped)
		return wrapped
	}

	e.mu.Lock()
	e.scripts = append(e.scripts, scriptEntry{name: name, code: compiled})
	e.mu.Unlock()

	e.ClearError()
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// Script execution
////////////////////////////////////////////////////////////////////////////////

// Execute runs all Python scripts previously loaded via Load/LoadMulti/LoadString
// in order. All scripts share the same module globals, so variables/functions
// defined in one script are visible to subsequent ones.
func (e *engine) Execute(_ context.Context) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrGPythonEngineNotInitialized)
		return nil, ErrGPythonEngineNotInitialized
	}

	e.mu.RLock()
	if len(e.scripts) == 0 {
		e.mu.RUnlock()
		e.setLastError(ErrGPythonNoScriptLoaded)
		return nil, ErrGPythonNoScriptLoaded
	}
	scripts := make([]scriptEntry, len(e.scripts))
	copy(scripts, e.scripts)
	e.mu.RUnlock()

	e.execMu.Lock()
	defer e.execMu.Unlock()

	if e.ctx == nil || e.module == nil {
		e.setLastError(ErrGPythonContextNotInitialized)
		return nil, ErrGPythonContextNotInitialized
	}

	for _, s := range scripts {
		_, err := e.ctx.RunCode(s.code, e.module.Globals, e.module.Globals, nil)
		if err != nil {
			wrapped := fmt.Errorf("%w: %s: %v", ErrGPythonRunFailed, s.name, err)
			e.setLastError(wrapped)
			return nil, wrapped
		}
	}

	e.ClearError()
	return nil, nil
}

// ExecuteFromKey loads the Python script identified by `key` from the bound
// Source and immediately runs it.
func (e *engine) ExecuteFromKey(ctx context.Context, key string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrGPythonEngineNotInitialized)
		return nil, ErrGPythonEngineNotInitialized
	}
	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("gpython engine: no source bound")
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

// ExecuteString compiles and immediately runs an inline Python source string.
func (e *engine) ExecuteString(ctx context.Context, name string, code string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrGPythonEngineNotInitialized)
		return nil, ErrGPythonEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()

	if e.ctx == nil || e.module == nil {
		e.setLastError(ErrGPythonContextNotInitialized)
		return nil, ErrGPythonContextNotInitialized
	}

	compiled, err := py.Compile(code, name, py.ExecMode, 0, true)
	if err != nil {
		wrapped := fmt.Errorf("%w: %s: %v", ErrGPythonCompileFailed, name, err)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	_, err = e.ctx.RunCode(compiled, e.module.Globals, e.module.Globals, nil)
	if err != nil {
		wrapped := fmt.Errorf("%w: %s: %v", ErrGPythonRunFailed, name, err)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	e.ClearError()
	return nil, nil
}

////////////////////////////////////////////////////////////////////////////////
// Global Variable Registration
////////////////////////////////////////////////////////////////////////////////

// RegisterGlobal registers or overwrites a named variable visible to Python
// scripts. The Go value is converted to a Python object automatically.
func (e *engine) RegisterGlobal(name string, value any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrGPythonEngineNotInitialized)
		return ErrGPythonEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()

	if e.module == nil {
		e.setLastError(ErrGPythonContextNotInitialized)
		return ErrGPythonContextNotInitialized
	}

	pyVal := goToPyObj(value)
	if pyVal == nil {
		pyVal = py.None
	}

	_, err := py.SetAttrString(e.module.Globals, name, pyVal)
	if err != nil {
		wrapped := fmt.Errorf("gpython engine: register global %q: %w", name, err)
		e.setLastError(wrapped)
		return wrapped
	}

	e.ClearError()
	return nil
}

// GetGlobal reads the value of a global variable from the Python module's
// globals dict. Returns the value converted to a Go type.
func (e *engine) GetGlobal(name string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrGPythonEngineNotInitialized)
		return nil, ErrGPythonEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()

	if e.module == nil {
		e.setLastError(ErrGPythonContextNotInitialized)
		return nil, ErrGPythonContextNotInitialized
	}

	obj, err := py.GetAttrString(e.module.Globals, name)
	if err != nil || obj == nil {
		wrapped := fmt.Errorf("%w: %s", ErrGPythonGlobalNotFound, name)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	e.ClearError()
	return pyObjToGo(obj), nil
}

////////////////////////////////////////////////////////////////////////////////
// Function Call
////////////////////////////////////////////////////////////////////////////////

// RegisterFunction registers a Go function that Python scripts can call by
// `name`. Due to gpython's architecture, functions are stored internally and
// injected as Python callables into the module globals.
func (e *engine) RegisterFunction(name string, fn any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrGPythonEngineNotInitialized)
		return ErrGPythonEngineNotInitialized
	}

	if reflect.TypeOf(fn).Kind() != reflect.Func {
		err := fmt.Errorf("gpython engine: RegisterFunction expects a func type, got %T", fn)
		e.setLastError(err)
		return err
	}

	e.mu.Lock()
	e.hostFuncs[name] = fn
	e.mu.Unlock()

	// Inject a Python-callable wrapper into the module globals.
	e.execMu.Lock()
	defer e.execMu.Unlock()

	if e.module == nil {
		e.setLastError(ErrGPythonContextNotInitialized)
		return ErrGPythonContextNotInitialized
	}

	wrapped := createPythonCallable(name, fn)
	_, err := py.SetAttrString(e.module.Globals, name, wrapped)
	if err != nil {
		wrappedErr := fmt.Errorf("gpython engine: register function %q: %w", name, err)
		e.setLastError(wrappedErr)
		return wrappedErr
	}

	e.ClearError()
	return nil
}

// CallFunction invokes a Python function by name from the module's globals.
func (e *engine) CallFunction(_ context.Context, name string, args ...any) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrGPythonEngineNotInitialized)
		return nil, ErrGPythonEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()

	if e.module == nil {
		e.setLastError(ErrGPythonContextNotInitialized)
		return nil, ErrGPythonContextNotInitialized
	}

	obj, err := py.GetAttrString(e.module.Globals, name)
	if err != nil || obj == nil {
		wrapped := fmt.Errorf("%w: %s", ErrGPythonFunctionNotFound, name)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	// Check if it's a host function stored internally.
	e.mu.RLock()
	hostFn, isHost := e.hostFuncs[name]
	e.mu.RUnlock()
	if isHost {
		result := callHostFunc(hostFn, args)
		e.ClearError()
		return result, nil
	}

	// Try to call as Python function by compiling and running call code.
	// Build a Python expression to call the function with args.
	callCode := buildPythonCallCode(name, len(args))
	compiled, compileErr := py.Compile(callCode, "call_"+name, py.EvalMode, 0, true)
	if compileErr != nil {
		wrapped := fmt.Errorf("%w: %s is not callable: %v", ErrGPythonFunctionNotFound, name, compileErr)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	// Set args as globals temporarily.
	for i, a := range args {
		argName := fmt.Sprintf("__arg%d__", i)
		_, _ = py.SetAttrString(e.module.Globals, argName, goToPyObj(a))
	}

	result, runErr := e.ctx.RunCode(compiled, e.module.Globals, e.module.Globals, nil)
	if runErr != nil {
		wrapped := fmt.Errorf("gpython engine: call %q failed: %w", name, runErr)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	e.ClearError()
	return pyObjToGo(result), nil
}

////////////////////////////////////////////////////////////////////////////////
// Module Management
////////////////////////////////////////////////////////////////////////////////

// RegisterModule registers a module from a map of values. Each key in the map
// becomes a global variable prefixed with the module name.
func (e *engine) RegisterModule(name string, module any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrGPythonEngineNotInitialized)
		return ErrGPythonEngineNotInitialized
	}

	syms, ok := module.(map[string]any)
	if !ok {
		err := fmt.Errorf("gpython engine: RegisterModule expects map[string]any, got %T", module)
		e.setLastError(err)
		return err
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()

	if e.module == nil {
		e.setLastError(ErrGPythonContextNotInitialized)
		return ErrGPythonContextNotInitialized
	}

	// Register each entry as a module-prefixed global.
	for key, val := range syms {
		globalName := name + "_" + key
		pyVal := goToPyObj(val)
		if pyVal == nil {
			pyVal = py.None
		}
		if _, err := py.SetAttrString(e.module.Globals, globalName, pyVal); err != nil {
			wrapped := fmt.Errorf("gpython engine: register module %q key %q: %w", name, key, err)
			e.setLastError(wrapped)
			return wrapped
		}
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

// StartWatch starts watching the Python script identified by `key` for changes.
func (e *engine) StartWatch(ctx context.Context, key string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrGPythonEngineNotInitialized)
		return ErrGPythonEngineNotInitialized
	}

	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("gpython engine: no source bound")
		e.setLastError(err)
		return err
	}

	watcher, ok := src.(source.Watcher)
	if !ok {
		err := fmt.Errorf("gpython engine: source does not implement Watcher")
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
				scriptEngine.GetLogger().Warn(watchCtx, "gpython engine: hot reload failed",
					"key", key, "error", loadErr)
			}
		}
	}()

	return nil
}

// StopWatch stops watching the Python script identified by `key`.
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
