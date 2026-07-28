// Package yaegi provides a script engine implementation using the Yaegi Go
// interpreter (https://github.com/traefik/yaegi). It allows executing Go source
// code as scripts at runtime without compilation.
//
// Construction:
//
//	eng, err := yaegi.New(ctx)
//	eng.Init(ctx)
//	eng.LoadString(ctx, "hello", `fmt.Println("hello world")`)
//	result, err := eng.Execute(ctx)
//
// The engine supports hot-reload via StartWatch/StopWatch when the bound Source
// implements the Watcher interface.
package yaegi

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/traefik/yaegi/interp"

	scriptEngine "github.com/tx7do/go-scripts"
	"github.com/tx7do/go-scripts/source"
)

func init() {
	_ = scriptEngine.Register(scriptEngine.YaegiType, func() (scriptEngine.Engine, error) {
		return newYaegiEngine()
	})
}

// engine is the Yaegi Go script engine implementation.
//
// Lock ordering convention (same as JavaScript engine):
//   - Always acquire `mu` (or its read lock) before `execMu`.
//   - Release in reverse order (execMu first, then mu).
//   - Never acquire `mu` while holding `execMu` to avoid deadlocks.
type engine struct {
	interp  *interp.Interpreter // the Yaegi interpreter
	scripts []string            // source code strings queued for execution

	// runtimeHooks are replayed on the interpreter right after Init, before
	// any Load*/Execute*. They let callers inject modules, host functions and
	// reverse callbacks. Guarded by mu.
	runtimeHooks []scriptEngine.RuntimeHook

	source      source.Reader // optional script source (File / S3 / Mem / ...)
	initialized bool
	lastError   error

	mu          sync.RWMutex // protects initialized, scripts and source
	execMu      sync.Mutex   // protects interp
	lastErrorMu sync.RWMutex // protects lastError

	// Hot reload state
	watchers   map[string]context.CancelFunc // key -> cancel func for the watch goroutine
	watchersMu sync.Mutex                    // protects watchers
}

// newYaegiEngine creates a Yaegi engine instance.
func newYaegiEngine() (*engine, error) {
	return &engine{
		initialized: false,
		watchers:    make(map[string]context.CancelFunc),
	}, nil
}

// GetType returns the script engine type.
func (e *engine) GetType() scriptEngine.Type {
	return scriptEngine.YaegiType
}

// Init initializes the engine.
func (e *engine) Init(ctx context.Context) error {
	// Set up the interpreter under the locks, then replay any hooks registered
	// before Init *after* releasing them — hooks call back into the engine
	// (e.g. RegisterFunction acquires execMu), which would self-deadlock if
	// we held the locks during replay.
	e.mu.Lock()
	if e.initialized {
		e.setLastError(ErrYaegiEngineAlreadyInitialized)
		e.mu.Unlock()
		return ErrYaegiEngineAlreadyInitialized
	}

	e.execMu.Lock()
	e.interp = interp.New(interp.Options{})
	e.initialized = true
	e.lastError = nil

	hooks := append([]scriptEngine.RuntimeHook(nil), e.runtimeHooks...)
	e.execMu.Unlock()
	e.mu.Unlock()

	// Replay hooks outside the engine locks.
	for _, h := range hooks {
		if err := h(ctx); err != nil {
			e.setLastError(err)
			return err
		}
	}

	return nil
}

// Close destroys the engine and releases underlying resources.
func (e *engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		e.setLastError(ErrYaegiEngineNotInitialized)
		return ErrYaegiEngineNotInitialized
	}

	// Stop all active watchers.
	e.stopAllWatchers()

	e.execMu.Lock()
	defer e.execMu.Unlock()

	e.initialized = false
	e.interp = nil
	e.scripts = nil

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

// SetSource binds a ScriptSource (FileSource / S3 / Mem / Multi / ...) to the
// engine. Subsequent Load / LoadMulti / ExecuteFromKey / ExecuteFromKeys calls
// read through it. Passing nil clears any previously bound source.
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

// Load loads a single script from the bound Source using the given key
// (path / object key / script id, ...). The loaded script is appended to the
// queue consumed later by Execute.
func (e *engine) Load(ctx context.Context, key string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrYaegiEngineNotInitialized)
		return ErrYaegiEngineNotInitialized
	}
	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("yaegi engine: no source bound")
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

// LoadMulti loads multiple scripts from the bound Source in order. It aborts on
// the first error.
func (e *engine) LoadMulti(ctx context.Context, keys []string) error {
	for _, k := range keys {
		if err := e.Load(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

// LoadString queues an inline Go source code string for later execution.
// `name` is used for diagnostics only; Yaegi does not use it internally.
// LoadString does NOT go through the bound Source.
func (e *engine) LoadString(_ context.Context, _ string, code string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrYaegiEngineNotInitialized)
		return ErrYaegiEngineNotInitialized
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.initialized {
		e.setLastError(ErrYaegiEngineNotInitialized)
		return ErrYaegiEngineNotInitialized
	}
	e.scripts = append(e.scripts, code)

	e.ClearError()
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// Script execution
////////////////////////////////////////////////////////////////////////////////

// Execute runs every script previously loaded via Load/LoadMulti/LoadString
// and returns the result of the last one. Yaegi evaluates each script fragment
// in order within the same interpreter context, so earlier fragments can define
// functions and variables used by later ones.
func (e *engine) Execute(ctx context.Context) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrYaegiEngineNotInitialized)
		return nil, ErrYaegiEngineNotInitialized
	}

	// Snapshot the scripts slice so concurrent LoadXxx calls cannot mutate it.
	e.mu.RLock()
	scripts := make([]string, len(e.scripts))
	copy(scripts, e.scripts)
	e.mu.RUnlock()

	if len(scripts) == 0 {
		e.setLastError(ErrYaegiNoScriptLoaded)
		return nil, ErrYaegiNoScriptLoaded
	}

	var lastResult any
	for _, src := range scripts {
		res, err := e.eval(ctx, src)
		if err != nil {
			return nil, err
		}
		lastResult = res
	}

	e.ClearError()
	return lastResult, nil
}

// ExecuteFromKey loads the script identified by `key` from the bound Source
// and immediately runs it, all in one step. The bound Source must be non-nil.
func (e *engine) ExecuteFromKey(ctx context.Context, key string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrYaegiEngineNotInitialized)
		return nil, ErrYaegiEngineNotInitialized
	}
	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("yaegi engine: no source bound")
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

// ExecuteFromKeys is the multi-key variant of ExecuteFromKey; results are
// returned in the same order as `keys`.
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

// ExecuteString compiles and immediately runs the inline Go source code,
// bypassing the bound Source. `name` is used for diagnostics only.
func (e *engine) ExecuteString(ctx context.Context, _ string, code string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrYaegiEngineNotInitialized)
		return nil, ErrYaegiEngineNotInitialized
	}
	return e.eval(ctx, code)
}

////////////////////////////////////////////////////////////////////////////////
// Globals, functions, modules
////////////////////////////////////////////////////////////////////////////////

// hostPkg is the synthetic package path used to expose globals and
// functions registered via RegisterGlobal / RegisterFunction to scripts.
// Scripts must `import "host"` to access them.
const hostPkg = "host"

// RegisterGlobal makes a Go value available in the interpreter under the given
// name. Internally this uses Yaegi's Exports mechanism: the value is registered
// in a synthetic "host" package. Scripts must import "host" to access the
// variable.
func (e *engine) RegisterGlobal(name string, value any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrYaegiEngineNotInitialized)
		return ErrYaegiEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.interp == nil {
		e.setLastError(ErrYaegiInterpreterNotInitialized)
		return ErrYaegiInterpreterNotInitialized
	}

	e.interp.Use(interp.Exports{
		hostPkg: map[string]reflect.Value{
			name: reflect.ValueOf(value),
		},
	})
	// Make the host package importable in this interpreter context.
	_, _ = e.interp.Eval(`import "` + hostPkg + `"`)

	e.ClearError()
	return nil
}

// GetGlobal reads a global variable from the interpreter. It first tries the
// interpreter scope (for variables defined by the script itself), then falls
// back to the "host" package (for variables registered via RegisterGlobal).
func (e *engine) GetGlobal(name string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrYaegiEngineNotInitialized)
		return nil, ErrYaegiEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.interp == nil {
		e.setLastError(ErrYaegiInterpreterNotInitialized)
		return nil, ErrYaegiInterpreterNotInitialized
	}

	// Try script-defined variable first.
	v, err := e.interp.Eval(name)
	if err != nil {
		// Fall back to the host package.
		v, err = e.interp.Eval(hostPkg + "." + name)
		if err != nil {
			wrapped := fmt.Errorf("%w: get global %q: %v", ErrYaegiEvalFailed, name, err)
			e.setLastError(wrapped)
			return nil, wrapped
		}
	}

	e.ClearError()
	return v.Interface(), nil
}

// RegisterFunction registers a Go function that scripts can call by `name`.
// The function must be a Go func type; it is registered in the "host" package.
// Scripts must `import "host"` and call `host.FuncName(args...)`.
func (e *engine) RegisterFunction(name string, fn any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrYaegiEngineNotInitialized)
		return ErrYaegiEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.interp == nil {
		e.setLastError(ErrYaegiInterpreterNotInitialized)
		return ErrYaegiInterpreterNotInitialized
	}

	// Verify fn is actually a function.
	if reflect.TypeOf(fn).Kind() != reflect.Func {
		err := fmt.Errorf("yaegi engine: RegisterFunction expects a func type, got %T", fn)
		e.setLastError(err)
		return err
	}

	e.interp.Use(interp.Exports{
		hostPkg: map[string]reflect.Value{
			name: reflect.ValueOf(fn),
		},
	})
	// Make the host package importable in this interpreter context.
	_, _ = e.interp.Eval(`import "` + hostPkg + `"`)

	e.ClearError()
	return nil
}

// CallFunction calls the named function with args and returns its result.
// The function must have been previously registered via RegisterFunction or
// defined in the "host" package. The name should NOT include the "host."
// prefix; it is added automatically.
func (e *engine) CallFunction(ctx context.Context, name string, args ...any) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrYaegiEngineNotInitialized)
		return nil, ErrYaegiEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.interp == nil {
		e.setLastError(ErrYaegiInterpreterNotInitialized)
		return nil, ErrYaegiInterpreterNotInitialized
	}

	// Look up the function: try script scope first, then host package, then
	// module-qualified names.
	var fnVal reflect.Value
	var evalErr error

	if strings.Contains(name, ".") {
		// Dotted name: treat as package.symbol (e.g. "mymath.Double").
		// Import the package first.
		parts := strings.SplitN(name, ".", 2)
		_, _ = e.interp.Eval(`import "` + parts[0] + `"`)
		fnVal, evalErr = e.interp.Eval(name)
	} else {
		// Bare name: try script scope first.
		fnVal, evalErr = e.interp.Eval(name)
		if evalErr != nil {
			// Fall back to host package.
			fnVal, evalErr = e.interp.Eval(hostPkg + "." + name)
		}
	}

	if evalErr != nil {
		wrapped := fmt.Errorf("%w: %v", ErrYaegiFunctionNotFound, evalErr)
		e.setLastError(wrapped)
		return nil, wrapped
	}

	if !fnVal.IsValid() || fnVal.Kind() != reflect.Func {
		err := fmt.Errorf("%w: %s is not a function", ErrYaegiFunctionNotFound, name)
		e.setLastError(err)
		return nil, err
	}

	// Convert args to reflect.Value.
	argVals := make([]reflect.Value, len(args))
	for i, a := range args {
		argVals[i] = reflect.ValueOf(a)
	}

	// Call the function.
	results := fnVal.Call(argVals)

	// Return the first result, or nil if no results.
	if len(results) == 0 {
		e.ClearError()
		return nil, nil
	}

	e.ClearError()
	return results[0].Interface(), nil
}

// RegisterModule registers a set of symbols under a package name so scripts
// can import them. `module` must be of type map[string]any; its keys become
// exported symbols in the named package.
func (e *engine) RegisterModule(name string, module any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrYaegiEngineNotInitialized)
		return ErrYaegiEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.interp == nil {
		e.setLastError(ErrYaegiInterpreterNotInitialized)
		return ErrYaegiInterpreterNotInitialized
	}

	syms, ok := module.(map[string]any)
	if !ok {
		err := fmt.Errorf("yaegi engine: RegisterModule expects map[string]any, got %T", module)
		e.setLastError(err)
		return err
	}

	// Convert map[string]any to map[string]reflect.Value.
	pkg := make(map[string]reflect.Value, len(syms))
	for k, v := range syms {
		pkg[k] = reflect.ValueOf(v)
	}

	e.interp.Use(interp.Exports{
		name: pkg,
	})
	// Make the package importable in this interpreter context.
	_, _ = e.interp.Eval(`import "` + name + `"`)

	e.ClearError()
	return nil
}

// AddRuntimeHook registers a RuntimeHook to run on the interpreter. If the
// engine is already initialized, the hook runs immediately; otherwise it is
// deferred until Init completes.
//
// Hooks typically inject business packages/symbols or reverse callbacks into
// the interpreter's "host" namespace (scripts access them via import "host").
func (e *engine) AddRuntimeHook(hook scriptEngine.RuntimeHook) error {
	if hook == nil {
		return nil
	}

	e.mu.Lock()
	e.runtimeHooks = append(e.runtimeHooks, hook)
	initialized := e.initialized
	e.mu.Unlock()

	if !initialized {
		// Deferred: Init will replay it.
		return nil
	}

	// Already initialized: run the hook immediately. Hooks usually call
	// RegisterFunction/RegisterModule, which take execMu themselves.
	if err := hook(context.Background()); err != nil {
		e.setLastError(err)
		return err
	}
	return nil
}

////////////////////////////////////////////////////////////////////////////////

// GetLastError returns the last error recorded by the engine.
func (e *engine) GetLastError() error {
	e.lastErrorMu.RLock()
	defer e.lastErrorMu.RUnlock()
	return e.lastError
}

// setLastError records the last error under lastErrorMu.
func (e *engine) setLastError(err error) {
	e.lastErrorMu.Lock()
	defer e.lastErrorMu.Unlock()
	e.lastError = err
}

// ClearError clears the last recorded error.
func (e *engine) ClearError() {
	e.lastErrorMu.Lock()
	defer e.lastErrorMu.Unlock()
	e.lastError = nil
}

////////////////////////////////////////////////////////////////////////////////
// Hot Reload (Watch)
////////////////////////////////////////////////////////////////////////////////

// StartWatch starts watching the script identified by `key` for changes.
// When the source reports a change, the script is automatically reloaded.
// Returns an error if the source is not bound or doesn't implement Watcher.
func (e *engine) StartWatch(ctx context.Context, key string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrYaegiEngineNotInitialized)
		return ErrYaegiEngineNotInitialized
	}

	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("yaegi engine: no source bound")
		e.setLastError(err)
		return err
	}

	watcher, ok := src.(source.Watcher)
	if !ok {
		err := fmt.Errorf("yaegi engine: source does not implement Watcher")
		e.setLastError(err)
		return err
	}

	// Stop existing watch for the same key if any.
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
			// Reload the script on change signal.
			if loadErr := e.Load(watchCtx, key); loadErr != nil {
				// Log but don't stop watching; the source may recover.
				scriptEngine.GetLogger().Warn(watchCtx, "yaegi engine: hot reload failed",
					"key", key, "error", loadErr)
			}
		}
	}()

	return nil
}

// StopWatch stops watching the script identified by `key`.
func (e *engine) StopWatch(key string) error {
	e.watchersMu.Lock()
	defer e.watchersMu.Unlock()
	if cancel, ok := e.watchers[key]; ok {
		cancel()
		delete(e.watchers, key)
	}
	return nil
}

// stopAllWatchers cancels all active watch goroutines. Called by Close.
func (e *engine) stopAllWatchers() {
	e.watchersMu.Lock()
	defer e.watchersMu.Unlock()
	for key, cancel := range e.watchers {
		cancel()
		delete(e.watchers, key)
	}
}
