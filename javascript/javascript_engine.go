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
//
// 锁使用约定：
// - 总是先获取 `mu`（或 `mu` 的写锁），然后再获取 `execMu`。
// - 释放顺序与获取顺序相反（先释放 `execMu`，再释放 `mu`）。
// - 不要在持有 `execMu` 的情况下再去获取 `mu`，以避免死锁。
// 该约定用于保护 runtime / programs / initialized 等状态的一致性。
type engine struct {
	runtime  *goja.Runtime   // JavaScript 运行时
	programs []*goja.Program // 已编译的程序列表

	initialized bool
	lastError   error

	mu          sync.RWMutex // 保护 initialized, programs
	execMu      sync.Mutex   // 保护 runtime
	lastErrorMu sync.RWMutex // 保护 lastError
}

// newJavascriptEngine 创建 JavaScript 引擎实例
func newJavascriptEngine() (*engine, error) {
	return &engine{
		initialized: false,
	}, nil
}

func (e *engine) GetType() scriptEngine.Type {
	return scriptEngine.JavaScriptType
}

// Init 初始化引擎
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

// Close 销毁引擎
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

// ClearPrograms 清空已缓存的已编译程序
func (e *engine) ClearPrograms() {
	e.mu.Lock()
	defer e.mu.Unlock()
	//e.program = nil
	e.programs = nil
}

// IsInitialized 检查是否已初始化
func (e *engine) IsInitialized() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.initialized
}

// LoadString 加载字符串脚本
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

// LoadFile 加载脚本文件
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

func (e *engine) LoadStrings(ctx context.Context, sources []string) error {
	for _, source := range sources {
		if err := e.LoadString(ctx, source); err != nil {
			return err
		}
	}
	return nil
}

func (e *engine) LoadFiles(ctx context.Context, filePaths []string) error {
	for _, filePath := range filePaths {
		if err := e.LoadFile(ctx, filePath); err != nil {
			return err
		}
	}
	return nil
}

// executeProgram 执行已编译的程序
func (e *engine) executeProgram(ctx context.Context, program *goja.Program) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return nil, ErrJavascriptEngineNotInitialized
	}

	return e.RunProgram(ctx, program)
}

// ExecuteLoaded 执行已加载的脚本
func (e *engine) ExecuteLoaded(ctx context.Context) (any, error) {
	if !e.IsInitialized() {
		e.setLastError(ErrJavascriptEngineNotInitialized)
		return nil, ErrJavascriptEngineNotInitialized
	}

	// 复制 programs 引用，避免执行期间被修改
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
			// RunProgram 已设置 lastError
			return nil, err
		}
		results = append(results, res)
	}

	e.ClearError()
	return results, nil
}

// ExecuteString 执行字符串脚本
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

// ExecuteFile 执行脚本文件
func (e *engine) ExecuteFile(ctx context.Context, filePath string) (any, error) {
	if err := e.LoadFile(ctx, filePath); err != nil {
		return nil, err
	}

	// ExecuteLoaded 返回一个结果切片（以 any 返回），兼容性处理末尾结果
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

// RegisterGlobal 注册全局变量
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

// GetGlobal 获取全局变量
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

// RegisterFunction 注册全局函数
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

// CallFunction 调用 JavaScript 函数
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

// RegisterModule 注册模块
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

// GetLastError 获取最后一个错误
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

// ClearError 清除错误
func (e *engine) ClearError() {
	e.lastErrorMu.Lock()
	defer e.lastErrorMu.Unlock()
	e.lastError = nil
}

// withRuntime 在受保护的环境中使用 runtime 执行函数
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
