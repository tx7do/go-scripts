package js

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/dop251/goja"

	scriptEngine "github.com/tx7do/go-scripts"
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

	initialized bool
	lastError   error

	mu          sync.RWMutex // protects initialized and programs
	execMu      sync.Mutex   // protects runtime
	lastErrorMu sync.RWMutex // protects lastError
}

// newJavascriptEngine creates a JavaScript engine instance.
func newJavascriptEngine() (*engine, error) {
	return &engine{
		initialized: false,
	}, nil
}

// GetType returns the script engine type.
func (e *engine) GetType() scriptEngine.Type {
	return scriptEngine.JavaScriptType
}

// Init initializes the engine.
func (e *engine) Init(_ context.Context) error {
	newRt := goja.New()

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.initialized {
		e.setLastError(ErrJavascriptEngineAlreadyInitialized)
		return ErrJavascriptEngineAlreadyInitialized
	}

	e.execMu.Lock()
	defer e.execMu.Unlock()

	e.runtime = newRt

	e.initialized = true
	e.lastError = nil

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

// LoadString compiles and queues a script given as a string source.
func (e *engine) LoadString(_ context.Context, source string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return ErrJavascriptEngineNotInitialized
	}

	program, err := goja.Compile("", source, true)
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

// LoadFile reads, compiles and queues a script from the given file path.
func (e *engine) LoadFile(_ context.Context, filePath string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return ErrJavascriptEngineNotInitialized
	}

	source, err := os.ReadFile(filePath)
	if err != nil {
		e.setLastError(err)
		return err
	}

	program, err := goja.Compile(filePath, string(source), true)
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

// LoadReader reads, compiles and queues a script from the given io.Reader.
func (e *engine) LoadReader(ctx context.Context, reader io.Reader, _ string) error {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return ErrJavascriptEngineNotInitialized
	}

	source, err := io.ReadAll(reader)
	if err != nil {
		e.setLastError(err)
		return err
	}

	return e.LoadString(ctx, string(source))
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

// executeProgram runs a single compiled program.
func (e *engine) executeProgram(ctx context.Context, program *goja.Program) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return nil, ErrJavascriptEngineNotInitialized
	}

	return e.RunProgram(ctx, program)
}

// ExecuteLoaded runs every program queued by LoadString/LoadFile/LoadReader
// and returns the collected results in order.
func (e *engine) ExecuteLoaded(ctx context.Context) (any, error) {
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

// ExecuteString compiles and immediately runs the given string source.
func (e *engine) ExecuteString(ctx context.Context, src string) (any, error) {
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
		var retErr error
		defer func() {
			if r := recover(); r != nil {
				retErr = fmt.Errorf("panic in ExecuteString: %v", r)
			}
		}()

		val, runErr := rt.RunString(src)
		if runErr != nil || val == nil {
			return nil, runErr
		}
		exported := val.Export()
		return exported, retErr
	})

	if err != nil {
		e.setLastError(err)
		return nil, err
	}
	e.ClearError()
	return result, nil
}

// ExecuteFile compiles and immediately runs the script at the given file path.
func (e *engine) ExecuteFile(ctx context.Context, filePath string) (any, error) {
	if err := e.LoadFile(ctx, filePath); err != nil {
		return nil, err
	}

	// ExecuteLoaded returns a slice of results (as any); keep only the last one
	// to match the single-file semantics.
	resAny, err := e.ExecuteLoaded(ctx)
	if err != nil {
		return nil, err
	}

	if arr, ok := resAny.([]any); ok {
		if len(arr) == 0 {
			return nil, nil
		}
		return arr[len(arr)-1], nil
	}
	return resAny, nil
}

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

// ExecuteStrings compiles and immediately runs multiple string sources,
// returning each result in order.
func (e *engine) ExecuteStrings(ctx context.Context, sources []string) ([]any, error) {
	results := make([]any, 0, len(sources))
	for _, src := range sources {
		res, err := e.ExecuteString(ctx, src)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	return results, nil
}

// ExecuteFiles compiles and immediately runs multiple file paths,
// returning each result in order.
func (e *engine) ExecuteFiles(ctx context.Context, filePaths []string) ([]any, error) {
	results := make([]any, 0, len(filePaths))
	for _, filePath := range filePaths {
		res, err := e.ExecuteFile(ctx, filePath)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	return results, nil
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
