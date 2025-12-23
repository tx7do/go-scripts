package script_engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

// EnginePool 管理多个独立 Engine 实例以支持并发执行。
// NewEnginePool 需要提供一个 factory 用于创建单个 Engine 实例。
type EnginePool struct {
	pool   chan Engine
	size   int
	mu     sync.Mutex
	closed bool
}

// NewEnginePool 创建并初始化一个包含 size 个 Engine 的池。
// factory 用于创建单个 Engine（例如 newLuaEngine）。
func NewEnginePool(size int, typ Type) (*EnginePool, error) {
	if size < 1 {
		return nil, errors.New("pool size must be >= 1")
	}

	p := &EnginePool{
		pool: make(chan Engine, size),
		size: size,
	}

	// 创建并初始化子 engine
	created := make([]Engine, 0, size)
	for i := 0; i < size; i++ {
		eng, err := NewScriptEngine(typ)
		if err != nil {
			// 清理已创建的 engines
			for _, e := range created {
				_ = e.Close()
			}
			return nil, fmt.Errorf("factory failed: %w", err)
		}

		// 调用 Init，失败则清理并返回
		if initErr := eng.Init(context.Background()); initErr != nil {
			_ = eng.Close()
			for _, e := range created {
				_ = e.Close()
			}
			return nil, fmt.Errorf("init failed: %w", initErr)
		}

		created = append(created, eng)
	}

	for _, e := range created {
		p.pool <- e
	}

	return p, nil
}

// Acquire 从池中获取一个 Engine（会阻塞直到有可用的）。
func (p *EnginePool) Acquire() (Engine, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("engine pool closed")
	}
	p.mu.Unlock()

	eng, ok := <-p.pool
	if !ok {
		return nil, errors.New("engine pool closed")
	}
	return eng, nil
}

// Release 将 Engine 放回池中；若池已关闭则关闭该 Engine。
func (p *EnginePool) Release(e Engine) {
	if e == nil {
		return
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()

	if closed {
		_ = e.Close()
		return
	}

	// 捕获并发 Close 导致的 send-on-closed panic
	defer func() {
		if r := recover(); r != nil {
			_ = e.Close()
		}
	}()

	select {
	case p.pool <- e:
	default:
		_ = e.Close()
	}
}

// Close 关闭池并销毁所有子 Engine。
func (p *EnginePool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.pool)
	p.mu.Unlock()

	var lastErr error
	for eng := range p.pool {
		if err := eng.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// IsClosed 返回池是否已关闭。
func (p *EnginePool) IsClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// 以下为常见包装方法：自动 acquire -> 调用 -> release。
// 若项目的 Engine 接口有所不同，可按需增减/调整。

func (p *EnginePool) InitAll(ctx context.Context) error {
	// 尝试获取池中所有实例
	engines := make([]Engine, 0, p.size)
	for i := 0; i < p.size; i++ {
		eng, err := p.Acquire()
		if err != nil {
			for _, e := range engines {
				_ = e.Close()
			}
			return err
		}
		engines = append(engines, eng)
	}

	// 对每个实例执行 Init()
	for _, eng := range engines {
		if err := eng.Init(ctx); err != nil {
			for _, e := range engines {
				_ = e.Close()
			}
			return fmt.Errorf("init failed: %w", err)
		}
	}

	// 释放回池
	for _, eng := range engines {
		p.Release(eng)
	}
	return nil
}

func (p *EnginePool) LoadString(ctx context.Context, source string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadString(ctx, source)
}

func (p *EnginePool) LoadFile(ctx context.Context, filePath string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadFile(ctx, filePath)
}

func (p *EnginePool) LoadReader(ctx context.Context, reader io.Reader, name string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadReader(ctx, reader, name)
}

func (p *EnginePool) LoadStrings(ctx context.Context, sources []string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadStrings(ctx, sources)
}

func (p *EnginePool) LoadFiles(ctx context.Context, filePaths []string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadFiles(ctx, filePaths)
}

func (p *EnginePool) ExecuteLoaded(ctx context.Context) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteLoaded(ctx)
}

func (p *EnginePool) ExecuteString(ctx context.Context, source string) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteString(ctx, source)
}

func (p *EnginePool) ExecuteFile(ctx context.Context, filePath string) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteFile(ctx, filePath)
}

func (p *EnginePool) ExecuteStrings(ctx context.Context, sources []string) ([]any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteStrings(ctx, sources)
}

func (p *EnginePool) ExecuteFiles(ctx context.Context, filePaths []string) ([]any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteFiles(ctx, filePaths)
}

func (p *EnginePool) RegisterGlobal(name string, value any) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.RegisterGlobal(name, value)
}

func (p *EnginePool) GetGlobal(name string) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.GetGlobal(name)
}

func (p *EnginePool) RegisterFunction(name string, fn any) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.RegisterFunction(name, fn)
}

func (p *EnginePool) CallFunction(ctx context.Context, name string, args ...any) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.CallFunction(ctx, name, args...)
}

func (p *EnginePool) RegisterModule(name string, module any) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.RegisterModule(name, module)
}

func (p *EnginePool) GetLastError() error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.GetLastError()
}

func (p *EnginePool) ClearError() {
	eng, err := p.Acquire()
	if err != nil {
		return
	}
	defer p.Release(eng)
	eng.ClearError()
}
