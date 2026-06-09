package lua

import (
	"context"
	"fmt"
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

// Init initializes the engine.
func (e *engine) Init(_ context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.initialized {
		e.setLastError(ErrLuaEngineAlreadyInitialized)
		return ErrLuaEngineAlreadyInitialized
	}

	e.vm = newVirtualMachine()
	e.initialized = true
	e.ClearError()

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

	e.vm.Destroy()
	e.vm = nil
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
		e.ClearError()
		return nil
	}

	err := fmt.Errorf("module must be of type Lua.LGFunction")
	e.setLastError(err)
	return err
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
