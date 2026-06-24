package lua

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	Lua "github.com/yuin/gopher-lua"

	scriptEngine "github.com/tx7do/go-scripts"
	"github.com/tx7do/go-scripts/source"
)

func init() {
	_ = scriptEngine.Register(scriptEngine.LuaType, func() (scriptEngine.Engine, error) {
		return newLuaEngine()
	})
}

// engine is the Lua script engine implementation.
type engine struct {
	vm          *virtualMachine
	initialized bool
	lastError   error

	source      source.Reader // optional script source (File / S3 / Mem / ...)
	mu          sync.RWMutex  // protects vm, initialized and source
	lastErrorMu sync.Mutex    // protects lastError

	// Runtime hooks are replayed on the LState right after Init, before any
	// Load*/Execute*. They let callers inject modules, host functions and
	// reverse callbacks. Guarded by mu.
	runtimeHooks []scriptEngine.RuntimeHook

	// businessGlobals records the global names registered via
	// RegisterGlobal / RegisterFunction / RegisterModule. These are stripped
	// from the LState before it is returned to the pool, so recycled LStates
	// don't leak a previous engine's business globals. Guarded by mu.
	businessGlobals map[string]struct{}

	// openLibs selects which standard libraries the VM opens at init. nil/empty
	// means "open all" (the backward-compatible default). Set via SetOpenLibs
	// to enable sandbox mode (drop dangerous libraries such as os/io/package).
	// See AllowedLib* constants. Guarded by mu.
	openLibs []string

	// quota is the execution budget applied to ExecuteSync runs. nil means
	// "no bound". Guarded by mu.
	quota *scriptEngine.Quota

	// Hot reload state
	watchers   map[string]context.CancelFunc // key -> cancel func for the watch goroutine
	watchersMu sync.Mutex                    // protects watchers
}

// newLuaEngine creates a Lua engine instance.
func newLuaEngine() (*engine, error) {
	return &engine{
		initialized: false,
		watchers:    make(map[string]context.CancelFunc),
	}, nil
}

// GetType returns the script engine type.
func (e *engine) GetType() scriptEngine.Type {
	return scriptEngine.LuaType
}

// SetOpenLibs configures which standard Lua libraries the VM opens at init.
//
// With no call (or nil/empty), all standard libraries open — the original,
// backward-compatible behavior.
//
// Pass an explicit list to enable sandbox mode and drop dangerous libraries.
// Valid names are the AllowedLib* constants (e.g. AllowedLibOs, AllowedLibIo,
// AllowedLibLoad). Unknown names are ignored. Must be called before Init.
//
// Example — open everything except os and io:
//
//	eng.SetOpenLibs(
//	    lua.AllowedLibBase, lua.AllowedLibLoad, lua.AllowedLibTab,
//	    lua.AllowedLibStr, lua.AllowedLibMath, lua.AllowedLibCoroutine,
//	)
func (e *engine) SetOpenLibs(libs ...string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.openLibs = libs
}

// Init initializes the engine.
func (e *engine) Init(ctx context.Context) error {
	// Create the VM under the lock, then replay any hooks registered before
	// Init *after* releasing the lock — hooks call back into the engine
	// (RegisterGlobal/RegisterFunction acquire e.mu), which would self-deadlock
	// if we held the lock during replay.
	e.mu.Lock()
	if e.initialized {
		e.setLastError(ErrLuaEngineAlreadyInitialized)
		e.mu.Unlock()
		return ErrLuaEngineAlreadyInitialized
	}

	e.vm = newVirtualMachine(e.openLibs)
	e.businessGlobals = make(map[string]struct{})
	e.initialized = true
	e.ClearError()

	hooks := append([]scriptEngine.RuntimeHook(nil), e.runtimeHooks...)
	e.mu.Unlock()

	// Replay hooks outside the engine lock.
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
		e.setLastError(ErrLuaEngineNotInitialized)
		return ErrLuaEngineNotInitialized
	}

	// Stop all active watchers.
	e.stopAllWatchers()

	// Strip business globals so the recycled LState doesn't leak them to a
	// future engine instance that borrows it from the pool.
	e.vm.ClearGlobals(e.snapshotBusinessGlobals())

	e.vm.Destroy()
	e.vm = nil
	e.businessGlobals = nil
	e.initialized = false

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
// (path / object key / script id, ...). The loaded script replaces the
// previously-compiled function and is run by the next Execute.
func (e *engine) Load(ctx context.Context, key string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrLuaEngineNotInitialized)
		return ErrLuaEngineNotInitialized
	}
	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("lua engine: no source bound")
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
// It aborts on the first error.
//
// Note: gopher-lua only keeps one compiled function at a time, so each Load
// overwrites the previous; use ExecuteFromKeys to run multiple scripts in order,
// or issue Load+Execute pairs manually.
func (e *engine) LoadMulti(ctx context.Context, keys []string) error {
	for _, k := range keys {
		if err := e.Load(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

// LoadString compiles and queues an inline script. `name` is accepted for
// interface compatibility but gopher-lua's LoadString does not use a name, so
// it is ignored. LoadString does NOT go through the bound Source.
func (e *engine) LoadString(_ context.Context, _ string, source string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		e.setLastError(ErrLuaEngineNotInitialized)
		return ErrLuaEngineNotInitialized
	}

	if err := e.vm.LoadString(source); err != nil {
		e.setLastError(err)
		return err
	}

	e.ClearError()

	return nil
}

////////////////////////////////////////////////////////////////////////////////
// Script execution
////////////////////////////////////////////////////////////////////////////////

// Execute runs the script previously loaded via Load/LoadMulti/LoadString.
// Context cancellation aborts the run.
//
// Note: gopher-lua only keeps a single compiled function at a time, so the
// returned result reflects the last load.
func (e *engine) Execute(ctx context.Context) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrLuaEngineNotInitialized)
		return nil, ErrLuaEngineNotInitialized
	}

	// Use a channel so the caller can cancel via ctx.
	done := make(chan error, 1)

	go func() {
		e.mu.Lock()
		defer e.mu.Unlock()

		if !e.initialized {
			done <- ErrLuaEngineNotInitialized
			return
		}

		done <- e.vm.Execute()
	}()

	select {
	case <-ctx.Done():
		e.setLastError(ctx.Err())
		return nil, ctx.Err()

	case err := <-done:
		if err != nil {
			e.setLastError(err)
			return nil, err
		}
		e.ClearError()
		return nil, nil
	}
}

// ExecuteFromKey loads the script identified by `key` from the bound Source
// and immediately runs it, all in one step. The bound Source must be non-nil.
func (e *engine) ExecuteFromKey(ctx context.Context, key string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrLuaEngineNotInitialized)
		return nil, ErrLuaEngineNotInitialized
	}
	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("lua engine: no source bound")
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
// bypassing the bound Source. `name` is accepted for interface compatibility but
// gopher-lua's DoString does not use a name, so it is ignored.
func (e *engine) ExecuteString(ctx context.Context, _ string, source string) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrLuaEngineNotInitialized)
		return nil, ErrLuaEngineNotInitialized
	}

	done := make(chan error, 1)

	go func() {
		e.mu.Lock()
		defer e.mu.Unlock()

		if !e.initialized {
			done <- ErrLuaEngineNotInitialized
			return
		}

		done <- e.vm.ExecuteString(source)
	}()

	select {
	case <-ctx.Done():
		e.setLastError(ctx.Err())
		return nil, ctx.Err()

	case err := <-done:
		if err != nil {
			e.setLastError(err)
			return nil, err
		}
		e.ClearError()
		return nil, nil
	}
}

////////////////////////////////////////////////////////////////////////////////
// Globals, functions, modules
////////////////////////////////////////////////////////////////////////////////

// RegisterGlobal binds a Go value into the Lua global scope under name.
func (e *engine) RegisterGlobal(name string, value any) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		e.setLastError(ErrLuaEngineNotInitialized)
		return ErrLuaEngineNotInitialized
	}

	e.vm.BindStruct(name, value)
	e.recordBusinessGlobal(name)

	e.ClearError()
	return nil
}

// GetGlobal reads a global Lua variable and converts it to a Go value.
func (e *engine) GetGlobal(name string) (any, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.initialized {
		e.setLastError(ErrLuaEngineNotInitialized)
		return nil, ErrLuaEngineNotInitialized
	}

	lv := e.vm.L.GetGlobal(name)
	result := e.vm.convertFromLValue(lv)
	e.ClearError()
	return result, nil
}

// RegisterFunction registers a global Lua function. fn must be of type
// Lua.LGFunction; any other type returns an error.
func (e *engine) RegisterFunction(name string, fn any) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		e.setLastError(ErrLuaEngineNotInitialized)
		return ErrLuaEngineNotInitialized
	}

	// Type assertion: only Lua.LGFunction is accepted.
	if lf, ok := fn.(Lua.LGFunction); ok {
		e.vm.RegisterFunction(name, lf)
		e.recordBusinessGlobal(name)
		e.ClearError()
		return nil
	}

	err := fmt.Errorf("function must be of type Lua.LGFunction")
	e.setLastError(err)
	return err
}

// CallFunction calls the named Lua function with args and returns its result.
func (e *engine) CallFunction(ctx context.Context, name string, args ...any) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrLuaEngineNotInitialized)
		return nil, ErrLuaEngineNotInitialized
	}

	type result struct {
		value any
		err   error
	}

	done := make(chan result, 1)

	go func() {
		e.mu.Lock()
		defer e.mu.Unlock()

		if !e.initialized {
			done <- result{nil, ErrLuaEngineNotInitialized}
			return
		}

		// Convert Go args to LValue.
		var lArgs []Lua.LValue
		for _, arg := range args {
			lArgs = append(lArgs, e.vm.convertToLValue(arg))
		}

		// Invoke the function.
		err := e.vm.L.CallByParam(Lua.P{
			Fn:      e.vm.L.GetGlobal(name),
			NRet:    1,
			Protect: true,
		}, lArgs...)

		if err != nil {
			done <- result{nil, err}
			return
		}

		// Pop the return value off the stack.
		ret := e.vm.L.Get(-1)
		e.vm.L.Pop(1)

		done <- result{e.vm.convertFromLValue(ret), nil}
	}()

	select {
	case <-ctx.Done():
		e.setLastError(ctx.Err())
		return nil, ctx.Err()

	case res := <-done:
		if res.err != nil {
			e.setLastError(res.err)
			return res.value, res.err
		}
		e.ClearError()
		return res.value, res.err
	}
}

// RegisterModule registers a Lua module. module must be of type Lua.LGFunction;
// any other type returns an error.
func (e *engine) RegisterModule(name string, module any) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		e.setLastError(ErrLuaEngineNotInitialized)
		return ErrLuaEngineNotInitialized
	}

	if mod, ok := module.(Lua.LGFunction); ok {
		e.vm.RegisterModule(name, mod)
		e.recordBusinessGlobal(name)
		e.ClearError()
		return nil
	}

	err := fmt.Errorf("module must be of type Lua.LGFunction")
	e.setLastError(err)
	return err
}

////////////////////////////////////////////////////////////////////////////////
// Runtime Hooks
////////////////////////////////////////////////////////////////////////////////

// AddRuntimeHook registers a RuntimeHook to run on the LState. If the engine is
// already initialized, the hook runs immediately; otherwise it is deferred
// until Init completes.
//
// Hooks typically inject business modules, host functions or reverse callbacks
// (e.g. a Go-side "hook.register" that scripts call to hand their callbacks
// back to Go). Globals/functions registered by hooks (or by any RegisterGlobal
// / RegisterFunction / RegisterModule call) are tracked as "business globals"
// and stripped from the LState before it returns to the pool, so recycled
// LStates stay isolated across engine instances.
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
	// RegisterFunction/RegisterModule, which take e.mu themselves, so we must
	// NOT hold it here.
	if err := hook(context.Background()); err != nil {
		e.setLastError(err)
		return err
	}
	return nil
}

// recordBusinessGlobal remembers a global name registered through the engine
// API so it can be cleared before the LState returns to the pool. Caller must
// hold e.mu.
func (e *engine) recordBusinessGlobal(name string) {
	if e.businessGlobals == nil {
		e.businessGlobals = make(map[string]struct{})
	}
	e.businessGlobals[name] = struct{}{}
}

// snapshotBusinessGlobals returns a copy of the tracked business-global names.
// Caller must hold e.mu.
func (e *engine) snapshotBusinessGlobals() []string {
	if len(e.businessGlobals) == 0 {
		return nil
	}
	names := make([]string, 0, len(e.businessGlobals))
	for name := range e.businessGlobals {
		names = append(names, name)
	}
	return names
}

////////////////////////////////////////////////////////////////////////////////
// Hot-path execution (SyncExecutor / QuotaController)
////////////////////////////////////////////////////////////////////////////////

// SetQuota configures the execution budget applied to subsequent ExecuteSync
// runs. The time budget (Quota.Timeout) is enforced via gopher-lua's
// context-checked main loop, which tests cancellation at every instruction —
// so a runaway script is interrupted mid-execution rather than blocking the
// host.
//
// NOTE on instruction limits: gopher-lua's debug.sethook is broken for count
// hooks (it raises "attempt to call a non-function object" regardless of
// mask), so Quota.MaxInstructions is currently NOT enforced by the Lua engine.
// Only Quota.Timeout is honored. Use a (tight) Timeout to bound execution.
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

// ExecuteSync runs the last-loaded script synchronously, without spawning a
// goroutine or allocating a channel — the minimal-overhead path for hot loops
// (e.g. game per-frame callbacks). It takes e.mu for the duration of the run
// (gopher-lua's LState is not safe for concurrent use), but avoids the
// goroutine/channel/select overhead of Execute.
//
// If a non-zero Quota.Timeout was set, it is applied via the LState's context
// (instruction-level cancellation). A run that exceeds its budget returns (or
// wraps) ErrQuotaExceeded. Quota.MaxInstructions is accepted but not enforced
// by the Lua engine (see SetQuota note).
//
// ctx, if it carries a deadline/cancellation, is also honored (merged with the
// quota timeout; the stricter deadline wins).
func (e *engine) ExecuteSync(ctx context.Context) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrLuaEngineNotInitialized)
		return nil, ErrLuaEngineNotInitialized
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized || e.vm == nil {
		e.setLastError(ErrLuaEngineNotInitialized)
		return nil, ErrLuaEngineNotInitialized
	}

	L := e.vm.L

	// Compose the time budget: the quota timeout and the caller's ctx. The
	// stricter (earlier) deadline wins. Setting ctx on the LState switches its
	// main loop to the per-instruction cancellation check.
	runCtx, cancel := e.composeRunCtx(ctx)
	if runCtx != nil {
		L.SetContext(runCtx)
		defer func() {
			cancel()
			L.RemoveContext()
		}()
	} else {
		cancel()
	}

	if err := e.vm.Execute(); err != nil {
		if isQuotaError(err) {
			wrapped := fmt.Errorf("%w: %v", scriptEngine.ErrQuotaExceeded, err)
			e.setLastError(wrapped)
			return nil, wrapped
		}
		e.setLastError(err)
		return nil, err
	}
	e.ClearError()
	return nil, nil
}

// composeRunCtx builds a context honoring both the quota timeout and the
// caller's ctx. It returns the context to set on the LState and a cancel func.
// Returns (nil, cancel) when neither imposes a deadline, meaning the LState
// needs no context wiring.
func (e *engine) composeRunCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	cancel := func() {}
	if e.quota == nil || e.quota.Timeout == 0 {
		return nil, cancel
	}
	quotaCtx, qCancel := context.WithTimeout(context.Background(), e.quota.Timeout)
	if ctx == nil {
		return quotaCtx, qCancel
	}
	// Merge: derive from the caller's ctx so its cancellation also aborts.
	merged, mCancel := context.WithTimeout(ctx, e.quota.Timeout)
	qCancel()
	return merged, mCancel
}

// isQuotaError reports whether err resulted from the time-quota mechanism.
// gopher-lua raises the context's error (context.DeadlineExceeded) as a Lua
// error via RaiseError, so we detect it by message contents as well as by
// errors.Is.
func isQuotaError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, scriptEngine.ErrQuotaExceeded) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "context deadline exceeded")
}

////////////////////////////////////////////////////////////////////////////////
// Error handling
////////////////////////////////////////////////////////////////////////////////

// GetLastError returns the last error recorded by the engine.
func (e *engine) GetLastError() error {
	e.lastErrorMu.Lock()
	defer e.lastErrorMu.Unlock()

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
		e.setLastError(ErrLuaEngineNotInitialized)
		return ErrLuaEngineNotInitialized
	}

	e.mu.RLock()
	src := e.source
	e.mu.RUnlock()
	if src == nil {
		err := fmt.Errorf("lua engine: no source bound")
		e.setLastError(err)
		return err
	}

	watcher, ok := src.(source.Watcher)
	if !ok {
		err := fmt.Errorf("lua engine: source does not implement Watcher")
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
				scriptEngine.GetLogger().Warn(watchCtx, "lua engine: hot reload failed",
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

// stopAllWatchers cancels all active watch goroutines. Caller must hold e.mu.
func (e *engine) stopAllWatchers() {
	e.watchersMu.Lock()
	defer e.watchersMu.Unlock()
	for key, cancel := range e.watchers {
		cancel()
		delete(e.watchers, key)
	}
}
