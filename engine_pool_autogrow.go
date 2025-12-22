package script_engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

// AutoGrowEnginePool 是可按需扩展但有上限的引擎池。
type AutoGrowEnginePool struct {
	pool chan Engine
	typ  Type

	mu     sync.Mutex
	total  int // 当前已创建的实例数
	max    int
	closed bool
}

// NewAutoGrowEnginePool 创建一个可自增长的池。
// initialSize: 初始创建数量（>=0）
// maxSize: 池允许的最大实例数（必须 >= initialSize && >=1）
func NewAutoGrowEnginePool(initialSize, maxSize int, typ Type) (*AutoGrowEnginePool, error) {
	if maxSize < 1 || initialSize < 0 || initialSize > maxSize {
		return nil, fmt.Errorf("invalid sizes: initial=%d max=%d", initialSize, maxSize)
	}
	if typ == "" {
		return nil, errors.New("engine type cannot be empty")
	}

	p := &AutoGrowEnginePool{
		pool:  make(chan Engine, maxSize), // 通道容量设为 maxSize
		typ:   typ,
		total: 0,
		max:   maxSize,
	}

	// 预创建 initialSize 个实例
	for i := 0; i < initialSize; i++ {
		eng, err := NewScriptEngine(typ)
		if err != nil {
			// 清理已创建的
		Drain:
			for {
				select {
				case e := <-p.pool:
					_ = e.Close()
				default:
					break Drain
				}
			}
			return nil, fmt.Errorf("script engine: factory failed: %w", err)
		}
		p.pool <- eng
		p.total++
	}

	return p, nil
}

// Acquire 获取一个 Engine：优先立即取空闲实例；若无且未到 max，则创建并返回新实例；否则阻塞等待。
func (p *AutoGrowEnginePool) Acquire() (Engine, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("script engine: engine pool closed")
	}
	p.mu.Unlock()

	// 尝试立即取一个空闲实例
	select {
	case eng := <-p.pool:
		return eng, nil
	default:
	}

	// 无空闲实例，尝试按需创建新实例（如果未到上限）
	p.mu.Lock()
	if p.total < p.max {
		p.total++
		p.mu.Unlock()
		eng, err := NewScriptEngine(p.typ)
		if err != nil {
			// 创建失败，回退计数
			p.mu.Lock()
			p.total--
			p.mu.Unlock()
			return nil, err
		}
		return eng, nil
	}
	// 已到上限，必须阻塞等待空闲实例
	p.mu.Unlock()
	eng, ok := <-p.pool
	if !ok {
		return nil, errors.New("script engine: engine pool closed")
	}
	return eng, nil
}

// Release 归还 Engine；若池已关闭或通道已满则关闭该实例。
func (p *AutoGrowEnginePool) Release(e Engine) {
	if e == nil {
		return
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()

	if closed {
		_ = e.Close()
		// 可选：根据语义决定是否在这里调整 total
		return
	}

	// 捕获 send-on-closed 的 panic，发生时安全关闭并尝试调整计数
	defer func() {
		if r := recover(); r != nil {
			_ = e.Close()
			p.mu.Lock()
			if p.total > 0 {
				p.total--
			}
			p.mu.Unlock()
		}
	}()

	select {
	case p.pool <- e:
	default:
		_ = e.Close()
		p.mu.Lock()
		if p.total > 0 {
			p.total--
		}
		p.mu.Unlock()
	}
}

// Close 关闭池并销毁所有空闲实例。已借出的实例应由调用方关闭或归还后会被关闭。
func (p *AutoGrowEnginePool) Close() error {
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

func (p *AutoGrowEnginePool) LoadString(ctx context.Context, source string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadString(ctx, source)
}

func (p *AutoGrowEnginePool) LoadFile(ctx context.Context, filePath string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadFile(ctx, filePath)
}

func (p *AutoGrowEnginePool) LoadReader(ctx context.Context, reader io.Reader, name string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadReader(ctx, reader, name)
}

func (p *AutoGrowEnginePool) Execute(ctx context.Context) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.Execute(ctx)
}

func (p *AutoGrowEnginePool) ExecuteString(ctx context.Context, source string) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteString(ctx, source)
}

func (p *AutoGrowEnginePool) ExecuteFile(ctx context.Context, filePath string) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteFile(ctx, filePath)
}

func (p *AutoGrowEnginePool) RegisterGlobal(name string, value any) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.RegisterGlobal(name, value)
}

func (p *AutoGrowEnginePool) GetGlobal(name string) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.GetGlobal(name)
}

func (p *AutoGrowEnginePool) RegisterFunction(name string, fn any) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.RegisterFunction(name, fn)
}

func (p *AutoGrowEnginePool) CallFunction(ctx context.Context, name string, args ...any) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.CallFunction(ctx, name, args...)
}

func (p *AutoGrowEnginePool) RegisterModule(name string, module any) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.RegisterModule(name, module)
}

func (p *AutoGrowEnginePool) GetLastError() error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.GetLastError()
}

func (p *AutoGrowEnginePool) ClearError() {
	eng, err := p.Acquire()
	if err != nil {
		return
	}
	defer p.Release(eng)
	eng.ClearError()
}
