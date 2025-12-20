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

// engine JavaScript 脚本引擎实现
type engine struct {
	runtime *goja.Runtime
	program *goja.Program

	initialized bool
	lastError   error

	mu     sync.RWMutex // 保护 initialized, program, lastError
	execMu sync.Mutex   // 保护 runtime 的并发访问（读锁用于运行/调用，写锁用于修改/关闭）
}

// newJavascriptEngine 创建 JavaScript 引擎实例
func newJavascriptEngine() (*engine, error) {
	return &engine{
		initialized: false,
	}, nil
}

// Init 初始化引擎
func (e *engine) Init(_ context.Context) error {
	newRt := goja.New()

	e.mu.Lock()
	if e.initialized {
		e.mu.Unlock()
		e.setLastError(ErrJavascriptEngineAlreadyInitialized)
		return ErrJavascriptEngineAlreadyInitialized
	}
	e.execMu.Lock()
	e.runtime = newRt
	e.execMu.Unlock()

	e.initialized = true
	e.lastError = nil
	e.mu.Unlock()

	return nil
}

// Close 销毁引擎
func (e *engine) Close() error {
	e.mu.Lock()
	if !e.initialized {
		e.mu.Unlock()
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return ErrJavascriptEngineNotInitialized
	}
	e.initialized = false
	e.mu.Unlock()

	e.execMu.Lock()
	e.runtime = nil
	e.program = nil
	e.execMu.Unlock()

	return nil
}

// IsInitialized 检查是否已初始化
func (e *engine) IsInitialized() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.initialized
}

// LoadString 加载字符串脚本
func (e *engine) LoadString(_ context.Context, source string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return ErrJavascriptEngineNotInitialized
	}

	program, err := goja.Compile("", source, true)
	if err != nil {
		e.setLastError(err)
		return err
	}

	e.program = program
	e.ClearError()
	return nil
}

// LoadFile 加载脚本文件
func (e *engine) LoadFile(_ context.Context, filePath string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
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

	e.program = program
	e.ClearError()
	return nil
}

// LoadReader 从 Reader 加载脚本
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

// Execute 执行已加载的脚本
func (e *engine) Execute(ctx context.Context) (any, error) {
	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return nil, ErrJavascriptEngineNotInitialized
	}
	if e.program == nil {
		e.mu.RUnlock()
		e.setLastError(ErrJavascriptNoProgramLoaded)
		return nil, ErrJavascriptNoProgramLoaded
	}
	prog := e.program
	e.mu.RUnlock()

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

	e.execMu.Lock()
	rt := e.runtime
	if rt == nil {
		e.execMu.Unlock()
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return nil, ErrJavascriptEngineNotInitialized
	}
	val, err := rt.RunProgram(prog)
	var result any
	if err == nil && val != nil {
		result = val.Export()
	}
	e.execMu.Unlock()

	if err != nil {
		e.setLastError(err)
		return nil, err
	}

	e.ClearError()

	return result, nil
}

// ExecuteString 执行字符串脚本
func (e *engine) ExecuteString(ctx context.Context, src string) (any, error) {
	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return nil, ErrJavascriptEngineNotInitialized
	}
	e.mu.RUnlock()

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

// ExecuteFile 执行脚本文件
func (e *engine) ExecuteFile(ctx context.Context, filePath string) (any, error) {
	if err := e.LoadFile(ctx, filePath); err != nil {
		return nil, err
	}
	return e.Execute(ctx)
}

// RegisterGlobal 注册全局变量
func (e *engine) RegisterGlobal(name string, value any) error {
	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return ErrJavascriptEngineNotInitialized
	}
	e.mu.RUnlock()

	e.execMu.Lock()
	if e.runtime == nil {
		e.execMu.Unlock()
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return ErrJavascriptEngineNotInitialized
	}
	_ = e.runtime.Set(name, value)
	e.execMu.Unlock()

	e.ClearError()

	return nil
}

// GetGlobal 获取全局变量
func (e *engine) GetGlobal(name string) (any, error) {
	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return nil, ErrJavascriptEngineNotInitialized
	}
	e.mu.RUnlock()

	e.execMu.Lock()
	if e.runtime == nil {
		e.execMu.Unlock()
		e.setLastError(ErrJavascriptRuntimeNotInitialized)
		return nil, ErrJavascriptRuntimeNotInitialized
	}
	val := e.runtime.Get(name)
	if val == nil {
		e.execMu.Unlock()
		err := fmt.Errorf("global variable %s not found", name)
		e.setLastError(err)
		return nil, err
	}
	result := val.Export()
	e.execMu.Unlock()

	e.ClearError()

	return result, nil
}

// RegisterFunction 注册全局函数
func (e *engine) RegisterFunction(name string, fn any) error {
	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return ErrJavascriptEngineNotInitialized
	}
	e.mu.RUnlock()

	e.execMu.Lock()
	if e.runtime == nil {
		e.execMu.Unlock()
		e.setLastError(ErrJavascriptRuntimeNotInitialized)
		return ErrJavascriptRuntimeNotInitialized
	}

	_ = e.runtime.Set(name, fn)
	e.execMu.Unlock()

	e.ClearError()

	return nil
}

// CallFunction 调用 JavaScript 函数
func (e *engine) CallFunction(ctx context.Context, name string, args ...any) (any, error) {
	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return nil, ErrJavascriptEngineNotInitialized
	}
	e.mu.RUnlock()

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

// RegisterModule 注册模块
func (e *engine) RegisterModule(name string, module any) error {
	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return ErrJavascriptEngineNotInitialized
	}
	e.mu.RUnlock()

	e.execMu.Lock()
	if e.runtime == nil {
		e.execMu.Unlock()
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
	e.execMu.Unlock()

	e.ClearError()

	return nil
}

// GetLastError 获取最后一个错误
func (e *engine) GetLastError() error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastError
}

func (e *engine) setLastError(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastError = err
}

// ClearError 清除错误
func (e *engine) ClearError() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastError = nil
}

// getRuntimeUnsafe 返回底层 *goja.Runtime（不安全）
// 调用者必须先获得 execMu 的适当锁（读或写），否则会导致并发问题。
func (e *engine) getRuntimeUnsafe() *goja.Runtime {
	return e.runtime
}

// withRuntime 提供一个在持 e.execMu 读锁下访问 runtime 的安全回调方式。
// 回调在持锁期间执行，返回值直接传出，避免将 *goja.Runtime 或 goja.Value
// 在释放锁后暴露给外部导致的并发问题。
func (e *engine) withRuntime(fn func(rt *goja.Runtime) (any, error)) (any, error) {
	e.execMu.Lock()
	defer e.execMu.Unlock()
	if e.runtime == nil {
		return nil, ErrJavascriptRuntimeNotInitialized
	}
	return fn(e.runtime)
}

// RunProgram 运行已编译的程序
func (e *engine) RunProgram(ctx context.Context, program *goja.Program) (any, error) {
	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return nil, ErrJavascriptEngineNotInitialized
	}
	e.mu.RUnlock()

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
