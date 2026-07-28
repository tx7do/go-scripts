package js

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dop251/goja"

	scriptEngine "github.com/tx7do/go-scripts"
	"github.com/tx7do/go-scripts/source"
)

func init() {
	_ = scriptEngine.Register(scriptEngine.JavaScriptType, func() (scriptEngine.Engine, error) {
		return newJavascriptEngine()
	})
}

// engine is the JavaScript script engine implementation.
//
// Lock ordering convention:
//   - Always acquire `mu` (or its read lock) before `execMu`.
//   - Release in reverse order (execMu first, then mu).
//   - Never acquire `mu` while holding `execMu` to avoid deadlocks.
//
// This convention protects the consistency of runtime / programs / initialized.
type engine struct {
	runtime  *goja.Runtime   // the JavaScript runtime
	programs []*goja.Program // compiled programs queued for execution

	// runtimeHooks are replayed on the runtime right after Init, before any
	// Load*/Execute*. They let callers inject modules, host functions and
	// reverse callbacks. Guarded by mu.
	runtimeHooks []scriptEngine.RuntimeHook

	source      source.Reader // optional script source (File / S3 / Mem / ...)
	initialized bool
	lastError   error

	mu          sync.RWMutex // protects initialized, programs, source and runtimeHooks
	execMu      sync.Mutex   // protects runtime
	lastErrorMu sync.RWMutex // protects lastError

	// quota is the execution budget applied to ExecuteSync runs. nil means
	// "no bound". Guarded by mu.
	quota *scriptEngine.Quota

	// Hot reload state
	watchers   map[string]context.CancelFunc // key -> cancel func for the watch goroutine
	watchersMu sync.Mutex                    // protects watchers
}

// newJavascriptEngine creates a JavaScript engine instance.
func newJavascriptEngine() (*engine, error) {
	return &engine{
		initialized: false,
		watchers:    make(map[string]context.CancelFunc),
	}, nil
}

// GetType returns the script engine type.
func (e *engine) GetType() scriptEngine.Type {
	return scriptEngine.JavaScriptType
}

// Init initializes the engine.
func (e *engine) Init(ctx context.Context) error {
	newRt := goja.New()

	// Set up the runtime under the locks, then replay any hooks registered
	// before Init *after* releasing them — hooks call back into the engine
	// (e.g. RegisterFunction acquires execMu), which would self-deadlock if
	// we held the locks during replay.
	e.mu.Lock()
	if e.initialized {
		e.setLastError(ErrJavascriptEngineAlreadyInitialized)
		e.mu.Unlock()
		return ErrJavascriptEngineAlreadyInitialized
	}

	e.execMu.Lock()
	e.runtime = newRt
	e.initialized = true
	e.lastError = nil

	hooks := append([]scriptEngine.RuntimeHook(nil), e.runtimeHooks...)
	e.execMu.Unlock()
	e.mu.Unlock()

	// Replay hooks outside the engine locks. Each hook typically calls
	// RegisterFunction/RegisterModule/RegisterGlobal, which take execMu
	// themselves, so we must not hold it here.
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
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return ErrJavascriptEngineNotInitialized
	}

	// Stop all active watchers.
	e.stopAllWatchers()

	e.execMu.Lock()
	defer e.execMu.Unlock()

	e.initialized = false
	e.runtime = nil
	e.programs = nil

	e.lastErrorMu.Lock()
	e.lastError = nil
	e.lastErrorMu.Unlock()

	return nil
}

// ClearPrograms drops all cached compiled programs.
func (e *engine) ClearPrograms() {
	e.mu.Lock()
	defer e.mu.Unlock()
	//e.program = nil
	e.programs = nil
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
func (e *engine) SetSource(source source.Reader) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.source = source
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
// (path / object key / script id, ...). The loaded program is appended to the
// queue consumed later by Execute.
func (e *engine) Load(ctx context.Context, key string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return ErrJavascriptEngineNotInitialized
	}
	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("javascript engine: no source bound")
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

// LoadString compiles and queues an inline script. `name` is used for
// diagnostics (stack traces, error messages); it is not used to look up the
// script on disk. LoadString does NOT go through the bound Source.
func (e *engine) LoadString(_ context.Context, name string, code string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return ErrJavascriptEngineNotInitialized
	}

	program, err := goja.Compile(name, code, true)
	if err != nil {
		e.setLastError(err)
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.initialized {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return ErrJavascriptEngineNotInitialized
	}
	e.programs = append(e.programs, program)

	e.ClearError()
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// Script execution
////////////////////////////////////////////////////////////////////////////////

// Execute runs every program previously loaded via Load/LoadMulti/LoadString
// and returns the collected results in order.
func (e *engine) Execute(ctx context.Context) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return nil, ErrJavascriptEngineNotInitialized
	}

	// Snapshot the programs slice so concurrent LoadXxx calls cannot mutate it
	// while we iterate.
	e.mu.RLock()
	progs := make([]*goja.Program, len(e.programs))
	copy(progs, e.programs)
	e.mu.RUnlock()

	if len(progs) == 0 {
		e.setLastError(ErrJavascriptNoProgramLoaded)
		return nil, ErrJavascriptNoProgramLoaded
	}

	results := make([]any, 0, len(progs))
	for _, p := range progs {
		res, err := e.RunProgram(ctx, p)
		if err != nil {
			// RunProgram already recorded lastError.
			return nil, err
		}
		results = append(results, res)
	}

	e.ClearError()
	return results, nil
}

// ExecuteFromKey loads the script identified by `key` from the bound Source
// and immediately runs it, all in one step. The bound Source must be non-nil.
func (e *engine) ExecuteFromKey(ctx context.Context, key string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return nil, ErrJavascriptEngineNotInitialized
	}
	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("javascript engine: no source bound")
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

// ExecuteString compiles and immediately runs the inline script (name+code),
// bypassing the bound Source. `name` is used for diagnostics (stack traces).
func (e *engine) ExecuteString(ctx context.Context, name string, code string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return nil, ErrJavascriptEngineNotInitialized
	}

	// Compile with the given name so stack traces point to a meaningful label.
	program, err := goja.Compile(name, code, true)
	if err != nil {
		e.setLastError(err)
		return nil, err
	}

	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-ctx.Done():
			e.execMu.Lock()
			rt := e.runtime
			e.execMu.Unlock()
			if rt != nil {
				rt.Interrupt(ctx.Err())
			}
		case <-done:
		}
	}()

	result, err := e.withRuntime(func(rt *goja.Runtime) (any, error) {
		var retErr error
		defer func() {
			if r := recover(); r != nil {
				retErr = fmt.Errorf("panic in ExecuteString: %v", r)
			}
		}()

		val, runErr := rt.RunProgram(program)
		if runErr != nil || val == nil {
			return nil, runErr
		}
		return val.Export(), retErr
	})

	if err != nil {
		e.setLastError(err)
		return nil, err
	}
	e.ClearError()
	return result, nil
}

////////////////////////////////////////////////////////////////////////////////
// Globals, functions, modules
////////////////////////////////////////////////////////////////////////////////

// RegisterGlobal registers a global variable in the JavaScript runtime.
func (e *engine) RegisterGlobal(name string, value any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return ErrJavascriptEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.runtime == nil {
		e.setLastError(ErrJavascriptRuntimeNotInitialized)
		return ErrJavascriptRuntimeNotInitialized
	}
	_ = e.runtime.Set(name, value)

	e.ClearError()

	return nil
}

// GetGlobal reads a global variable from the JavaScript runtime.
func (e *engine) GetGlobal(name string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return nil, ErrJavascriptEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.runtime == nil {
		e.setLastError(ErrJavascriptRuntimeNotInitialized)
		return nil, ErrJavascriptRuntimeNotInitialized
	}
	val := e.runtime.Get(name)
	if val == nil {
		err := fmt.Errorf("global variable %s not found", name)
		e.setLastError(err)
		return nil, err
	}
	result := val.Export()

	e.ClearError()

	return result, nil
}

// RegisterFunction registers a global function in the JavaScript runtime.
func (e *engine) RegisterFunction(name string, fn any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return ErrJavascriptEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.runtime == nil {
		e.setLastError(ErrJavascriptRuntimeNotInitialized)
		return ErrJavascriptRuntimeNotInitialized
	}

	_ = e.runtime.Set(name, fn)

	e.ClearError()

	return nil
}

// CallFunction calls the named function with args and returns its result.
func (e *engine) CallFunction(ctx context.Context, name string, args ...any) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return nil, ErrJavascriptEngineNotInitialized
	}

	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-ctx.Done():
			e.execMu.Lock()
			rt := e.runtime
			e.execMu.Unlock()
			if rt != nil {
				rt.Interrupt(ctx.Err())
			}
		case <-done:
		}
	}()

	result, err := e.withRuntime(func(rt *goja.Runtime) (any, error) {
		var (
			res    any
			retErr error
		)
		defer func() {
			if r := recover(); r != nil {
				retErr = fmt.Errorf("panic in CallFunction %s: %v", name, r)
			}
		}()

		v := rt.Get(name)
		if v == nil {
			return nil, fmt.Errorf("function %s not found", name)
		}
		fn, ok := goja.AssertFunction(v)
		if !ok {
			return nil, fmt.Errorf("%s is not a function", name)
		}

		vals := make([]goja.Value, len(args))
		for i, a := range args {
			vals[i] = rt.ToValue(a)
		}

		callRes, callErr := fn(goja.Undefined(), vals...)
		if callErr != nil {
			return nil, callErr
		}
		if callRes == nil {
			return nil, nil
		}
		res = callRes.Export()
		return res, retErr
	})

	if err != nil {
		e.setLastError(err)
		return nil, err
	}
	e.ClearError()
	return result, nil
}

// RegisterModule registers a module (map[string]any or raw value) under name.
func (e *engine) RegisterModule(name string, module any) error {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return ErrJavascriptEngineNotInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.runtime == nil {
		e.setLastError(ErrJavascriptRuntimeNotInitialized)
		return ErrJavascriptRuntimeNotInitialized
	}

	moduleObj := e.runtime.NewObject()
	if m, ok := module.(map[string]any); ok {
		for k, v := range m {
			_ = moduleObj.Set(k, v)
		}
		_ = e.runtime.Set(name, moduleObj)
	} else {
		_ = e.runtime.Set(name, module)
	}

	e.ClearError()

	return nil
}

// AddRuntimeHook registers a RuntimeHook to run on the runtime. If the engine
// is already initialized, the hook runs immediately on the live runtime;
// otherwise it is deferred until Init completes.
//
// Hooks typically inject business modules, host functions or reverse callbacks
// (e.g. a Go-side "register" function that scripts call to hand their
// callbacks back to Go).
func (e *engine) AddRuntimeHook(hook scriptEngine.RuntimeHook) error {
	if hook == nil {
		return nil
	}

	// Snapshot initialized state under mu and append the hook.
	e.mu.Lock()
	e.runtimeHooks = append(e.runtimeHooks, hook)
	initialized := e.initialized
	e.mu.Unlock()

	if !initialized {
		// Deferred: Init will replay it.
		return nil
	}

	// Already initialized: run the hook immediately on the live runtime.
	// Hooks usually call RegisterFunction/RegisterModule, which take execMu
	// themselves, so we must NOT hold execMu here.
	if err := hook(context.Background()); err != nil {
		e.setLastError(err)
		return err
	}
	return nil
}

////////////////////////////////////////////////////////////////////////////////
// Hot-path execution (SyncExecutor / QuotaController)
////////////////////////////////////////////////////////////////////////////////

// SetQuota configures the execution budget applied to subsequent ExecuteSync
// runs. The time budget (Quota.Timeout) is enforced via goja's Interrupt:
// a watcher goroutine arms a timer and interrupts the runtime when it fires,
// aborting the run mid-execution. Quota.MaxInstructions is not enforced by the
// JS engine (goja has no instruction counter).
//
// Pass a zero-value Quota to remove any bound.
func (e *engine) SetQuota(q scriptEngine.Quota) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if q.Timeout == 0 && q.MaxInstructions == 0 {
		e.quota = nil
	} else {
		cp := q
		e.quota = &cp
	}
}

// ExecuteSync runs every program previously loaded via Load/LoadMulti/LoadString
// synchronously, without spawning a goroutine per call — the minimal-overhead
// path for hot loops (e.g. game per-frame callbacks). It avoids the per-call
// goroutine/channel/select overhead of Execute.
//
// If a non-zero Quota.Timeout was set, a watcher goroutine arms a timer and
// interrupts the goja runtime when it elapses; the interrupted run returns (or
// wraps) ErrQuotaExceeded. Quota.MaxInstructions is not enforced by the JS
// engine.
//
// Note: the watcher goroutine is only spawned when a timeout quota is set, so
// the zero-overhead path is preserved when no quota is configured.
func (e *engine) ExecuteSync(ctx context.Context) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return nil, ErrJavascriptEngineNotInitialized
	}

	// Snapshot programs.
	e.mu.RLock()
	progs := make([]*goja.Program, len(e.programs))
	copy(progs, e.programs)
	quota := e.quota
	e.mu.RUnlock()

	if len(progs) == 0 {
		e.setLastError(ErrJavascriptNoProgramLoaded)
		return nil, ErrJavascriptNoProgramLoaded
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.runtime == nil {
		e.setLastError(ErrJavascriptRuntimeNotInitialized)
		return nil, ErrJavascriptRuntimeNotInitialized
	}

	// Arm the quota watcher if a timeout is configured. The watcher interrupts
	// the runtime when the timer fires or the caller's ctx is cancelled. We pass
	// the runtime reference directly so the watcher never needs execMu (which we
	// hold here for the whole run) — rt.Interrupt is safe to call concurrently.
	var stopWatch chan struct{}
	if quota != nil && quota.Timeout > 0 {
		stopWatch = make(chan struct{})
		rt := e.runtime
		go e.armQuotaWatch(ctx, quota.Timeout, rt, stopWatch)
	}

	var results []any
	for _, p := range progs {
		val, err := e.runtime.RunProgram(p)
		if err != nil {
			if stopWatch != nil {
				close(stopWatch)
			}
			if isJSQuotaError(err) {
				wrapped := fmt.Errorf("%w: %v", scriptEngine.ErrQuotaExceeded, err)
				e.setLastError(wrapped)
				return nil, wrapped
			}
			e.setLastError(err)
			return nil, err
		}
		if val != nil {
			results = append(results, val.Export())
		}
	}
	if stopWatch != nil {
		close(stopWatch)
	}
	e.ClearError()
	return results, nil
}

// armQuotaWatch interrupts the runtime after timeout, or when ctx is cancelled.
// It returns when stop is closed, after which the caller owns cleanup. This
// goroutine is only spawned when a timeout quota is active. It must NOT take
// execMu (the caller holds it for the whole run); rt.Interrupt is safe to call
// concurrently with execution.
func (e *engine) armQuotaWatch(ctx context.Context, timeout time.Duration, rt *goja.Runtime, stop <-chan struct{}) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-timer.C:
		if rt != nil {
			rt.Interrupt(scriptEngine.ErrQuotaExceeded)
		}
	case <-ctx.Done():
		if rt != nil {
			rt.Interrupt(ctx.Err())
		}
	case <-stop:
	}
}

// isJSQuotaError reports whether err resulted from a quota/timeout interruption
// (goja surfaces InterruptedError with the value passed to Interrupt).
func isJSQuotaError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, scriptEngine.ErrQuotaExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// goja returns an InterruptedError whose string carries the interrupt value.
	msg := err.Error()
	return msg == scriptEngine.ErrQuotaExceeded.Error() ||
		msg == context.DeadlineExceeded.Error()
}

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

// withRuntime runs fn while holding execMu, providing safe access to the runtime.
func (e *engine) withRuntime(fn func(rt *goja.Runtime) (any, error)) (any, error) {
	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.runtime == nil {
		return nil, ErrJavascriptRuntimeNotInitialized
	}
	return fn(e.runtime)
}

// RunProgram runs a compiled goja program with context cancellation support.
func (e *engine) RunProgram(ctx context.Context, program *goja.Program) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return nil, ErrJavascriptEngineNotInitialized
	}

	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-ctx.Done():
			e.execMu.Lock()
			rt := e.runtime
			e.execMu.Unlock()
			if rt != nil {
				rt.Interrupt(ctx.Err())
			}
		case <-done:
		}
	}()

	result, err := e.withRuntime(func(rt *goja.Runtime) (any, error) {
		val, err := rt.RunProgram(program)
		if err != nil || val == nil {
			return nil, err
		}
		return val.Export(), nil
	})

	if err != nil {
		e.setLastError(err)
		return nil, err
	}

	e.ClearError()

	return result, nil
}

////////////////////////////////////////////////////////////////////////////////
// Hot Reload (Watch)
////////////////////////////////////////////////////////////////////////////////

// StartWatch starts watching the script identified by `key` for changes.
// When the source reports a change, the script is automatically reloaded.
// Returns an error if the source is not bound or doesn't implement Watcher.
func (e *engine) StartWatch(ctx context.Context, key string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return ErrJavascriptEngineNotInitialized
	}

	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("javascript engine: no source bound")
		e.setLastError(err)
		return err
	}

	watcher, ok := src.(source.Watcher)
	if !ok {
		err := fmt.Errorf("javascript engine: source does not implement Watcher")
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
				scriptEngine.GetLogger().Warn(watchCtx, "javascript engine: hot reload failed",
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
