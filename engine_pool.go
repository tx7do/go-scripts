package script_engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

// EnginePool manages a fixed number of independent Engine instances to support
// concurrent execution. The pool is created via NewEnginePool, which uses the
// factory registered for typ to instantiate each Engine.
type EnginePool struct {
	pool   chan Engine
	size   int
	mu     sync.Mutex
	closed bool
}

// NewEnginePool creates and initializes a pool of size Engines.
// typ selects the registered factory used to create each Engine.
func NewEnginePool(size int, typ Type) (*EnginePool, error) {
	if size < 1 {
		return nil, errors.New("pool size must be >= 1")
	}

	p := &EnginePool{
		pool: make(chan Engine, size),
		size: size,
	}

	// Create and initialize each Engine; clean up on failure.
	created := make([]Engine, 0, size)
	for i := 0; i < size; i++ {
		eng, err := NewScriptEngine(typ)
		if err != nil {
			// Clean up already-created engines.
			for _, e := range created {
				_ = e.Close()
			}
			return nil, fmt.Errorf("factory failed: %w", err)
		}

		// Call Init; on failure clean up and bail out.
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

// Acquire takes an Engine out of the pool. It blocks until one becomes available.
// Returns an error if the pool is already closed.
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

// Release returns an Engine to the pool. If the pool is already closed, the
// Engine is Closed instead.
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

	// Guard against send-on-closed panic caused by concurrent Close.
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

// Close closes the pool and destroys all pooled Engines.
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

// IsClosed reports whether the pool has been closed.
func (p *EnginePool) IsClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// The following methods are common wrappers that follow the pattern
//     Acquire -> invoke -> Release.
// They exist so callers can avoid the Acquire/Release boilerplate for trivial
// one-shot calls. Adjust or remove them to match your Engine interface.

// InitAll re-initializes every Engine in the pool.
// It acquires all instances, calls Init on each, and releases them back.
func (p *EnginePool) InitAll(ctx context.Context) error {
	// Acquire every instance in the pool.
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

	// Call Init on each instance.
	for _, eng := range engines {
		if err := eng.Init(ctx); err != nil {
			for _, e := range engines {
				_ = e.Close()
			}
			return fmt.Errorf("init failed: %w", err)
		}
	}

	// Release them back to the pool.
	for _, eng := range engines {
		p.Release(eng)
	}
	return nil
}

// LoadString loads a script from a string into an acquired Engine.
func (p *EnginePool) LoadString(ctx context.Context, source string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadString(ctx, source)
}

// LoadFile loads a script from a file path into an acquired Engine.
func (p *EnginePool) LoadFile(ctx context.Context, filePath string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadFile(ctx, filePath)
}

// LoadReader loads a script from an io.Reader into an acquired Engine.
func (p *EnginePool) LoadReader(ctx context.Context, reader io.Reader, name string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadReader(ctx, reader, name)
}

// LoadStrings loads multiple scripts from string sources into an acquired Engine.
func (p *EnginePool) LoadStrings(ctx context.Context, sources []string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadStrings(ctx, sources)
}

// LoadFiles loads multiple scripts from file paths into an acquired Engine.
func (p *EnginePool) LoadFiles(ctx context.Context, filePaths []string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadFiles(ctx, filePaths)
}

// ExecuteLoaded runs the previously loaded script(s) on an acquired Engine.
func (p *EnginePool) ExecuteLoaded(ctx context.Context) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteLoaded(ctx)
}

// ExecuteString runs the given script source on an acquired Engine.
func (p *EnginePool) ExecuteString(ctx context.Context, source string) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteString(ctx, source)
}

// ExecuteFile runs the script at the given file path on an acquired Engine.
func (p *EnginePool) ExecuteFile(ctx context.Context, filePath string) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteFile(ctx, filePath)
}

// ExecuteStrings runs multiple script sources on an acquired Engine and returns
// each result in order.
func (p *EnginePool) ExecuteStrings(ctx context.Context, sources []string) ([]any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteStrings(ctx, sources)
}

// ExecuteFiles runs multiple script files on an acquired Engine and returns each
// result in order.
func (p *EnginePool) ExecuteFiles(ctx context.Context, filePaths []string) ([]any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteFiles(ctx, filePaths)
}

// RegisterGlobal registers a global variable on an acquired Engine.
// Note: the registration is local to that Engine instance.
func (p *EnginePool) RegisterGlobal(name string, value any) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.RegisterGlobal(name, value)
}

// GetGlobal reads a global variable from an acquired Engine.
func (p *EnginePool) GetGlobal(name string) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.GetGlobal(name)
}

// RegisterFunction registers a function on an acquired Engine.
// Note: the registration is local to that Engine instance.
func (p *EnginePool) RegisterFunction(name string, fn any) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.RegisterFunction(name, fn)
}

// CallFunction calls the named function on an acquired Engine with the given args.
func (p *EnginePool) CallFunction(ctx context.Context, name string, args ...any) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.CallFunction(ctx, name, args...)
}

// RegisterModule registers a module on an acquired Engine.
// Note: the registration is local to that Engine instance.
func (p *EnginePool) RegisterModule(name string, module any) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.RegisterModule(name, module)
}

// GetLastError returns the last error from an acquired Engine.
func (p *EnginePool) GetLastError() error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.GetLastError()
}

// ClearError clears the last error on an acquired Engine.
func (p *EnginePool) ClearError() {
	eng, err := p.Acquire()
	if err != nil {
		return
	}
	defer p.Release(eng)
	eng.ClearError()
}
