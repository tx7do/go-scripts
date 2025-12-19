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

// engine Lua 脚本引擎实现
type engine struct {
	vm          *virtualMachine
	initialized bool
	lastError   error
	mu          sync.Mutex
}

// newLuaEngine 创建 Lua 引擎实例
func newLuaEngine() (*engine, error) {
	return &engine{
		initialized: false,
	}, nil
}

// Init 初始化引擎
func (e *engine) Init(_ context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.initialized {
		return ErrLuaEngineAlreadyInitialized
	}

	e.vm = newVirtualMachine()
	e.initialized = true
	e.lastError = nil

	return nil
}

// Close 销毁引擎
func (e *engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return ErrLuaEngineNotInitialized
	}

	e.vm.Destroy()
	e.vm = nil
	e.initialized = false

	return nil
}

// IsInitialized 检查是否已初始化
func (e *engine) IsInitialized() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.initialized
}

// LoadString 加载字符串脚本
func (e *engine) LoadString(_ context.Context, source string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return ErrLuaEngineNotInitialized
	}

	if err := e.vm.LoadString(source); err != nil {
		e.lastError = err
		return err
	}

	return nil
}

// LoadFile 加载脚本文件
func (e *engine) LoadFile(_ context.Context, filePath string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return ErrLuaEngineNotInitialized
	}

	if err := e.vm.LoadFile(filePath); err != nil {
		e.lastError = err
		return err
	}

	return nil
}

// LoadReader 从 Reader 加载脚本
func (e *engine) LoadReader(ctx context.Context, reader io.Reader, _ string) error {
	source, err := io.ReadAll(reader)
	if err != nil {
		e.lastError = err
		return err
	}

	return e.LoadString(ctx, string(source))
}

// Execute 执行已加载的脚本
func (e *engine) Execute(ctx context.Context) (any, error) {
	e.mu.Lock()
	if !e.initialized {
		e.mu.Unlock()
		return nil, ErrLuaEngineNotInitialized
	}
	e.mu.Unlock()

	// 使用 channel 处理超时
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
		e.mu.Lock()
		e.lastError = ctx.Err()
		e.mu.Unlock()
		return nil, ctx.Err()

	case err := <-done:
		if err != nil {
			e.mu.Lock()
			e.lastError = err
			e.mu.Unlock()
			return nil, err
		}
		return nil, nil
	}
}

// ExecuteString 执行字符串脚本
func (e *engine) ExecuteString(ctx context.Context, source string) (any, error) {
	// 先用锁检查 initialized，避免竞态
	e.mu.Lock()
	if !e.initialized {
		e.mu.Unlock()
		return nil, ErrLuaEngineNotInitialized
	}
	e.mu.Unlock()

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
		e.mu.Lock()
		e.lastError = ctx.Err()
		e.mu.Unlock()
		return nil, ctx.Err()

	case err := <-done:
		if err != nil {
			e.mu.Lock()
			e.lastError = err
			e.mu.Unlock()
			return nil, err
		}
		return nil, nil
	}
}

// ExecuteFile 执行脚本文件
func (e *engine) ExecuteFile(ctx context.Context, filePath string) (any, error) {
	e.mu.Lock()
	if !e.initialized {
		e.mu.Unlock()
		return nil, ErrLuaEngineNotInitialized
	}
	e.mu.Unlock()

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
		e.mu.Lock()
		e.lastError = ctx.Err()
		e.mu.Unlock()
		return nil, ctx.Err()

	case err := <-done:
		if err != nil {
			e.mu.Lock()
			e.lastError = err
			e.mu.Unlock()
			return nil, err
		}
		return nil, nil
	}
}

// RegisterGlobal 注册全局变量
func (e *engine) RegisterGlobal(name string, value any) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return ErrLuaEngineNotInitialized
	}

	e.vm.BindStruct(name, value)
	return nil
}

// GetGlobal 获取全局变量
func (e *engine) GetGlobal(name string) (any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return nil, ErrLuaEngineNotInitialized
	}

	lv := e.vm.L.GetGlobal(name)
	return e.vm.convertFromLValue(lv), nil
}

// RegisterFunction 注册全局函数
func (e *engine) RegisterFunction(name string, fn any) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return ErrLuaEngineNotInitialized
	}

	// 类型断言检查是否为 Lua.LGFunction
	if lf, ok := fn.(Lua.LGFunction); ok {
		e.vm.RegisterFunction(name, lf)
		return nil
	}

	return fmt.Errorf("function must be of type Lua.LGFunction")
}

// CallFunction 调用 Lua 函数
func (e *engine) CallFunction(ctx context.Context, name string, args ...any) (any, error) {
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

		// 转换参数
		var lArgs []Lua.LValue
		for _, arg := range args {
			lArgs = append(lArgs, e.vm.convertToLValue(arg))
		}

		// 调用函数
		err := e.vm.L.CallByParam(Lua.P{
			Fn:      e.vm.L.GetGlobal(name),
			NRet:    1,
			Protect: true,
		}, lArgs...)

		if err != nil {
			done <- result{nil, err}
			return
		}

		// 获取返回值
		ret := e.vm.L.Get(-1)
		e.vm.L.Pop(1)

		done <- result{e.vm.convertFromLValue(ret), nil}
	}()

	select {
	case <-ctx.Done():
		e.mu.Lock()
		e.lastError = ctx.Err()
		e.mu.Unlock()
		return nil, ctx.Err()
	case res := <-done:
		if res.err != nil {
			e.mu.Lock()
			e.lastError = res.err
			e.mu.Unlock()
		}
		return res.value, res.err
	}
}

// RegisterModule 注册模块
func (e *engine) RegisterModule(name string, module any) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return ErrLuaEngineNotInitialized
	}

	if mod, ok := module.(Lua.LGFunction); ok {
		e.vm.RegisterModule(name, mod)
		return nil
	}

	return fmt.Errorf("module must be of type Lua.LGFunction")
}

// GetLastError 获取最后一个错误
func (e *engine) GetLastError() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.lastError
}

// ClearError 清除错误
func (e *engine) ClearError() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.lastError = nil
}
