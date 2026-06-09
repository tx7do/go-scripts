package script_engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

// AutoGrowEnginePool is an Engine pool that can grow on demand up to a configured
// maximum. Idle instances are reused; when none is available and the cap has not
// been reached, a new Engine is created on the fly.
type AutoGrowEnginePool struct {
	pool chan Engine
	typ  Type

	mu     sync.Mutex
	total  int // number of Engine instances currently alive
	max    int
	closed bool
}

// NewAutoGrowEnginePool creates a pool that can grow on demand.
//   - initialSize: number of Engines to create eagerly (>= 0)
//   - maxSize: upper bound on instance count (must be >= initialSize and >= 1)
//   - typ: selects the registered factory used to build each Engine
func NewAutoGrowEnginePool(initialSize, maxSize int, typ Type) (*AutoGrowEnginePool, error) {
	if maxSize < 1 || initialSize < 0 || initialSize > maxSize {
		return nil, fmt.Errorf("invalid sizes: initial=%d max=%d", initialSize, maxSize)
	}
	if typ == "" {
		return nil, errors.New("engine type cannot be empty")
	}

	p := &AutoGrowEnginePool{
		pool:  make(chan Engine, maxSize), // channel capacity equals the cap
		typ:   typ,
		total: 0,
		max:   maxSize,
	}

	// Build the initial set of Engines; on any failure clean them all up.
	created := make([]Engine, 0, initialSize)
	for i := 0; i < initialSize; i++ {
		eng, err := NewScriptEngine(typ)
		if err != nil {
			for _, e := range created {
				_ = e.Close()
			}
			return nil, fmt.Errorf("script engine: factory failed: %w", err)
		}

		if initErr := eng.Init(context.Background()); initErr != nil {
			_ = eng.Close()
			for _, e := range created {
				_ = e.Close()
			}
			return nil, fmt.Errorf("script engine: init failed: %w", initErr)
		}

		created = append(created, eng)
	}

	// All initial instances were created and initialized successfully; push them
	// into the channel and commit the total counter.
	for _, e := range created {
		p.pool <- e
	}
	p.total = len(created)

	return p, nil
}

// Acquire returns an Engine from the pool.
//   - If an idle instance is available, it is returned immediately.
//   - Otherwise, if total < max, a new instance is created and returned.
//   - Otherwise the call blocks until an instance is released.
func (p *AutoGrowEnginePool) Acquire() (Engine, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("script engine: engine pool closed")
	}
	p.mu.Unlock()

	// Fast path: grab an idle instance if one is immediately available.
	select {
	case eng := <-p.pool:
		return eng, nil
	default:
	}

	// No idle instance; try to grow the pool if the cap allows.
	p.mu.Lock()
	if p.total < p.max {
		p.total++
		p.mu.Unlock()
		eng, err := NewScriptEngine(p.typ)
		if err != nil {
			// Creation failed; roll back the counter.
			p.mu.Lock()
			p.total--
			p.mu.Unlock()
			return nil, err
		}

		// Initialize the freshly created engine.
		if initErr := eng.Init(context.Background()); initErr != nil {
			_ = eng.Close()
			p.mu.Lock()
			p.total--
			p.mu.Unlock()
			return nil, initErr
		}

		return eng, nil
	}
	// Cap reached; must wait for a released instance.
	p.mu.Unlock()

	eng, ok := <-p.pool
	if !ok {
		return nil, errors.New("script engine: engine pool closed")
	}

	return eng, nil
}

// Release returns an Engine to the pool. If the pool is closed or the channel
// is full, the Engine is Closed instead and the live counter is decremented.
func (p *AutoGrowEnginePool) Release(e Engine) {
	if e == nil {
		return
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()

	if closed {
		_ = e.Close()
		// Optional: decide whether to adjust `total` here based on semantics.
		return
	}

	// Guard against send-on-closed panic; if it fires, close the Engine and try
	// to keep the live counter consistent.
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

// Close closes the pool and destroys every currently idle instance.
// Engines that are still borrowed must be Closed by their callers (or will be
// Closed when eventually released).
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

// LoadString loads a script from a string into an acquired Engine.
func (p *AutoGrowEnginePool) LoadString(ctx context.Context, source string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadString(ctx, source)
}

// LoadFile loads a script from a file path into an acquired Engine.
func (p *AutoGrowEnginePool) LoadFile(ctx context.Context, filePath string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadFile(ctx, filePath)
}

// LoadReader loads a script from an io.Reader into an acquired Engine.
func (p *AutoGrowEnginePool) LoadReader(ctx context.Context, reader io.Reader, name string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadReader(ctx, reader, name)
}

// LoadStrings loads multiple scripts from string sources into an acquired Engine.
func (p *AutoGrowEnginePool) LoadStrings(ctx context.Context, sources []string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadStrings(ctx, sources)
}

// LoadFiles loads multiple scripts from file paths into an acquired Engine.
func (p *AutoGrowEnginePool) LoadFiles(ctx context.Context, filePaths []string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadFiles(ctx, filePaths)
}

// ExecuteLoaded runs the previously loaded script(s) on an acquired Engine.
func (p *AutoGrowEnginePool) ExecuteLoaded(ctx context.Context) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteLoaded(ctx)
}

// ExecuteString runs the given script source on an acquired Engine.
func (p *AutoGrowEnginePool) ExecuteString(ctx context.Context, source string) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteString(ctx, source)
}

// ExecuteFile runs the script at the given file path on an acquired Engine.
func (p *AutoGrowEnginePool) ExecuteFile(ctx context.Context, filePath string) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteFile(ctx, filePath)
}

// ExecuteStrings runs multiple script sources on an acquired Engine and returns
// each result in order.
func (p *AutoGrowEnginePool) ExecuteStrings(ctx context.Context, sources []string) ([]any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteStrings(ctx, sources)
}

// ExecuteFiles runs multiple script files on an acquired Engine and returns each
// result in order.
func (p *AutoGrowEnginePool) ExecuteFiles(ctx context.Context, filePaths []string) ([]any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteFiles(ctx, filePaths)
}

// RegisterGlobal registers a global variable on an acquired Engine.
// Note: the registration is local to that Engine instance.
func (p *AutoGrowEnginePool) RegisterGlobal(name string, value any) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.RegisterGlobal(name, value)
}

// GetGlobal reads a global variable from an acquired Engine.
func (p *AutoGrowEnginePool) GetGlobal(name string) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.GetGlobal(name)
}

// RegisterFunction registers a function on an acquired Engine.
// Note: the registration is local to that Engine instance.
func (p *AutoGrowEnginePool) RegisterFunction(name string, fn any) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.RegisterFunction(name, fn)
}

// CallFunction calls the named function on an acquired Engine with the given args.
func (p *AutoGrowEnginePool) CallFunction(ctx context.Context, name string, args ...any) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.CallFunction(ctx, name, args...)
}

// RegisterModule registers a module on an acquired Engine.
// Note: the registration is local to that Engine instance.
func (p *AutoGrowEnginePool) RegisterModule(name string, module any) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.RegisterModule(name, module)
}

// GetLastError returns the last error from an acquired Engine.
func (p *AutoGrowEnginePool) GetLastError() error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.GetLastError()
}

// ClearError clears the last error on an acquired Engine.
func (p *AutoGrowEnginePool) ClearError() {
	eng, err := p.Acquire()
	if err != nil {
		return
	}
	defer p.Release(eng)
	eng.ClearError()
}
