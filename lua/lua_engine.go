package lua

import (
	"context"
	"fmt"
	"io"

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
}

// newLuaEngine 创建 Lua 引擎实例
func newLuaEngine() (*engine, error) {
	return &engine{
		initialized: false,
	}, nil
}

// Init 初始化引擎
func (e *engine) Init(_ context.Context) error {
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
	return e.initialized
}

// LoadString 加载字符串脚本
func (e *engine) LoadString(_ context.Context, source string) error {
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
	if !e.initialized {
		return ErrLuaEngineNotInitialized
	}

	source, err := io.ReadAll(reader)
	if err != nil {
		e.lastError = err
		return err
	}

	return e.LoadString(ctx, string(source))
}

// Execute 执行已加载的脚本
func (e *engine) Execute(ctx context.Context) (any, error) {
	if !e.initialized {
		return nil, ErrLuaEngineNotInitialized
	}

	// 使用 channel 处理超时
	done := make(chan error, 1)

	go func() {
		done <- e.vm.Execute()
	}()

	select {
	case <-ctx.Done():
		e.lastError = ctx.Err()
		return nil, ctx.Err()
	case err := <-done:
		if err != nil {
			e.lastError = err
			return nil, err
		}
		return nil, nil
	}
}

// ExecuteString 执行字符串脚本
func (e *engine) ExecuteString(ctx context.Context, source string) (any, error) {
	if !e.initialized {
		return nil, ErrLuaEngineNotInitialized
	}

	done := make(chan error, 1)

	go func() {
		done <- e.vm.ExecuteString(source)
	}()

	select {
	case <-ctx.Done():
		e.lastError = ctx.Err()
		return nil, ctx.Err()
	case err := <-done:
		if err != nil {
			e.lastError = err
			return nil, err
		}
		return nil, nil
	}
}

// ExecuteFile 执行脚本文件
func (e *engine) ExecuteFile(ctx context.Context, filePath string) (any, error) {
	if !e.initialized {
		return nil, ErrLuaEngineNotInitialized
	}

	done := make(chan error, 1)

	go func() {
		done <- e.vm.ExecuteFile(filePath)
	}()

	select {
	case <-ctx.Done():
		e.lastError = ctx.Err()
		return nil, ctx.Err()
	case err := <-done:
		if err != nil {
			e.lastError = err
			return nil, err
		}
		return nil, nil
	}
}

// RegisterGlobal 注册全局变量
func (e *engine) RegisterGlobal(name string, value any) error {
	if !e.initialized {
		return ErrLuaEngineNotInitialized
	}

	e.vm.BindStruct(name, value)
	return nil
}

// GetGlobal 获取全局变量
func (e *engine) GetGlobal(name string) (any, error) {
	if !e.initialized {
		return nil, ErrLuaEngineNotInitialized
	}

	lv := e.vm.L.GetGlobal(name)
	return e.vm.convertFromLValue(lv), nil
}

// RegisterFunction 注册全局函数
func (e *engine) RegisterFunction(name string, fn any) error {
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
	if !e.initialized {
		return nil, ErrLuaEngineNotInitialized
	}

	type result struct {
		value any
		err   error
	}

	done := make(chan result, 1)

	go func() {
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
		e.lastError = ctx.Err()
		return nil, ctx.Err()
	case res := <-done:
		if res.err != nil {
			e.lastError = res.err
		}
		return res.value, res.err
	}
}

// RegisterModule 注册模块
func (e *engine) RegisterModule(name string, module any) error {
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
	return e.lastError
}

// ClearError 清除错误
func (e *engine) ClearError() {
	e.lastError = nil
}

// GetState 获取 Lua 状态机（扩展方法）
func (e *engine) GetState() *Lua.LState {
	if e.vm == nil {
		return nil
	}
	return e.vm.L
}
