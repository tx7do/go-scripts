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
	runtime     *goja.Runtime
	program     *goja.Program
	initialized bool
	lastError   error
	mu          sync.RWMutex
}

// newJavascriptEngine 创建 JavaScript 引擎实例
func newJavascriptEngine() (*engine, error) {
	return &engine{
		initialized: false,
	}, nil
}

// Init 初始化引擎
func (e *engine) Init(_ context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.initialized {
		return ErrJavascriptEngineAlreadyInitialized
	}

	e.runtime = goja.New()
	e.initialized = true
	e.lastError = nil

	return nil
}

// Close 销毁引擎
func (e *engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return ErrJavascriptEngineNotInitialized
	}

	e.runtime = nil
	e.program = nil
	e.initialized = false

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
		return ErrJavascriptEngineNotInitialized
	}

	program, err := goja.Compile("", source, true)
	if err != nil {
		e.lastError = err
		return err
	}

	e.program = program
	return nil
}

// LoadFile 加载脚本文件
func (e *engine) LoadFile(_ context.Context, filePath string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return ErrJavascriptEngineNotInitialized
	}

	source, err := os.ReadFile(filePath)
	if err != nil {
		e.lastError = err
		return err
	}

	program, err := goja.Compile(filePath, string(source), true)
	if err != nil {
		e.lastError = err
		return err
	}

	e.program = program
	return nil
}

// LoadReader 从 Reader 加载脚本
func (e *engine) LoadReader(ctx context.Context, reader io.Reader, _ string) error {
	if !e.initialized {
		return ErrJavascriptEngineNotInitialized
	}

	source, err := io.ReadAll(reader)
	if err != nil {
		e.lastError = err
		return err
	}

	return e.LoadString(ctx, string(source))
}

// Execute 执行已加载的脚本
func (e *engine) Execute(ctx context.Context) (interface{}, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return nil, ErrJavascriptEngineNotInitialized
	}

	if e.program == nil {
		return nil, fmt.Errorf("no program loaded")
	}

	type result struct {
		value goja.Value
		err   error
	}

	done := make(chan result, 1)

	go func() {
		val, err := e.runtime.RunProgram(e.program)
		done <- result{val, err}
	}()

	select {
	case <-ctx.Done():
		e.runtime.Interrupt(ctx.Err())
		e.lastError = ctx.Err()
		return nil, ctx.Err()
	case res := <-done:
		if res.err != nil {
			e.lastError = res.err
			return nil, res.err
		}
		return res.value.Export(), nil
	}
}

// ExecuteString 执行字符串脚本
func (e *engine) ExecuteString(ctx context.Context, source string) (interface{}, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return nil, ErrJavascriptEngineNotInitialized
	}

	type result struct {
		value goja.Value
		err   error
	}

	done := make(chan result, 1)

	go func() {
		val, err := e.runtime.RunString(source)
		done <- result{val, err}
	}()

	select {
	case <-ctx.Done():
		e.runtime.Interrupt(ctx.Err())
		e.lastError = ctx.Err()
		return nil, ctx.Err()
	case res := <-done:
		if res.err != nil {
			e.lastError = res.err
			return nil, res.err
		}
		return res.value.Export(), nil
	}
}

// ExecuteFile 执行脚本文件
func (e *engine) ExecuteFile(ctx context.Context, filePath string) (interface{}, error) {
	if err := e.LoadFile(ctx, filePath); err != nil {
		return nil, err
	}
	return e.Execute(ctx)
}

// RegisterGlobal 注册全局变量
func (e *engine) RegisterGlobal(name string, value interface{}) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return ErrJavascriptEngineNotInitialized
	}

	_ = e.runtime.Set(name, value)
	return nil
}

// GetGlobal 获取全局变量
func (e *engine) GetGlobal(name string) (interface{}, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.initialized {
		return nil, ErrJavascriptEngineNotInitialized
	}

	val := e.runtime.Get(name)
	if val == nil {
		return nil, fmt.Errorf("global variable %s not found", name)
	}

	return val.Export(), nil
}

// RegisterFunction 注册全局函数
func (e *engine) RegisterFunction(name string, fn interface{}) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return ErrJavascriptEngineNotInitialized
	}

	_ = e.runtime.Set(name, fn)
	return nil
}

// CallFunction 调用 JavaScript 函数
func (e *engine) CallFunction(ctx context.Context, name string, args ...interface{}) (interface{}, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return nil, ErrJavascriptEngineNotInitialized
	}

	type result struct {
		value goja.Value
		err   error
	}

	done := make(chan result, 1)

	go func() {
		fn, ok := goja.AssertFunction(e.runtime.Get(name))
		if !ok {
			done <- result{nil, fmt.Errorf("function %s not found", name)}
			return
		}

		// 转换参数
		var gojaArgs []goja.Value
		for _, arg := range args {
			gojaArgs = append(gojaArgs, e.runtime.ToValue(arg))
		}

		val, err := fn(goja.Undefined(), gojaArgs...)
		done <- result{val, err}
	}()

	select {
	case <-ctx.Done():
		e.runtime.Interrupt(ctx.Err())
		e.lastError = ctx.Err()
		return nil, ctx.Err()
	case res := <-done:
		if res.err != nil {
			e.lastError = res.err
			return nil, res.err
		}
		return res.value.Export(), nil
	}
}

// RegisterModule 注册模块
func (e *engine) RegisterModule(name string, module interface{}) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return ErrJavascriptEngineNotInitialized
	}

	// 创建模块对象
	moduleObj := e.runtime.NewObject()

	// 如果 module 是 map，则设置属性
	if m, ok := module.(map[string]interface{}); ok {
		for k, v := range m {
			_ = moduleObj.Set(k, v)
		}
	} else {
		// 否则直接设置整个对象
		_ = e.runtime.Set(name, module)
		return nil
	}

	_ = e.runtime.Set(name, moduleObj)
	return nil
}

// GetLastError 获取最后一个错误
func (e *engine) GetLastError() error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastError
}

// ClearError 清除错误
func (e *engine) ClearError() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastError = nil
}

// GetRuntime 获取 Goja 运行时（扩展方法）
func (e *engine) GetRuntime() *goja.Runtime {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.runtime
}

// RunProgram 运行已编译的程序（扩展方法）
func (e *engine) RunProgram(ctx context.Context, program *goja.Program) (goja.Value, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return nil, ErrJavascriptEngineNotInitialized
	}

	type result struct {
		value goja.Value
		err   error
	}

	done := make(chan result, 1)

	go func() {
		val, err := e.runtime.RunProgram(program)
		done <- result{val, err}
	}()

	select {
	case <-ctx.Done():
		e.runtime.Interrupt(ctx.Err())
		return nil, ctx.Err()
	case res := <-done:
		return res.value, res.err
	}
}
