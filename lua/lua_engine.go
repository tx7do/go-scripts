package lua

import (
	"context"
	"fmt"
	"io"
	"sync"

	Lua "github.com/yuin/gopher-lua"

	scriptEngine "github.com/tx7do/go-scripts"
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

	mu          sync.RWMutex
	lastErrorMu sync.Mutex
}

// newLuaEngine creates a Lua engine instance.
func newLuaEngine() (*engine, error) {
	return &engine{
		initialized: false,
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

// LoadString compiles and queues a script given as a string source.
func (e *engine) LoadString(_ context.Context, source string) error {
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

// LoadStrings compiles and queues multiple scripts from string sources.
func (e *engine) LoadStrings(ctx context.Context, sources []string) error {
	for _, source := range sources {
		if err := e.LoadString(ctx, source); err != nil {
			return err
		}
	}
	return nil
}

// LoadFiles compiles and queues multiple scripts from file paths.
func (e *engine) LoadFiles(ctx context.Context, filePaths []string) error {
	for _, filePath := range filePaths {
		if err := e.LoadFile(ctx, filePath); err != nil {
			return err
		}
	}
	return nil
}

// LoadFile reads, compiles and queues a script from the given file path.
func (e *engine) LoadFile(_ context.Context, filePath string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		e.setLastError(ErrLuaEngineNotInitialized)
		return ErrLuaEngineNotInitialized
	}

	if err := e.vm.LoadFile(filePath); err != nil {
		e.setLastError(err)
		return err
	}

	e.ClearError()

	return nil
}

// LoadReader reads, compiles and queues a script from the given io.Reader.
func (e *engine) LoadReader(ctx context.Context, reader io.Reader, _ string) error {
	source, err := io.ReadAll(reader)
	if err != nil {
		e.setLastError(err)
		return err
	}

	return e.LoadString(ctx, string(source))
}

// ExecuteLoaded runs every script queued by LoadString/LoadFile/LoadReader.
// Context cancellation aborts the run.
func (e *engine) ExecuteLoaded(ctx context.Context) (any, error) {
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

// ExecuteStrings compiles and immediately runs multiple string sources,
// returning each result in order.
func (e *engine) ExecuteStrings(ctx context.Context, sources []string) ([]any, error) {
	var results []any
	for _, source := range sources {
		result, err := e.ExecuteString(ctx, source)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

// ExecuteFiles compiles and immediately runs multiple file paths,
// returning each result in order.
func (e *engine) ExecuteFiles(ctx context.Context, filePaths []string) ([]any, error) {
	var results []any
	for _, filePath := range filePaths {
		result, err := e.ExecuteFile(ctx, filePath)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

// ExecuteString compiles and immediately runs the given string source.
func (e *engine) ExecuteString(ctx context.Context, source string) (any, error) {
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

// ExecuteFile compiles and immediately runs the script at the given file path.
func (e *engine) ExecuteFile(ctx context.Context, filePath string) (any, error) {
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

		done <- e.vm.ExecuteFile(filePath)
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
