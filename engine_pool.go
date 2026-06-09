package script_engine

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/tx7do/go-scripts/source"
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
// one-shot calls. They mirror the current Engine interface 1:1.
//
// IMPORTANT: per-call wrappers acquire a single Engine, perform the operation,
// and release it back. Stateful bindings (SetSource, RegisterGlobal,
// RegisterFunction, RegisterModule) are therefore LOCAL TO THAT ENGINE INSTANCE.
// If you need a binding to apply to every Engine in the pool, iterate over
// Acquire/Release yourself or pre-configure each Engine before pooling.

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

////////////////////////////////////////////////////////////////////////////////
// ScriptSource injection & access
////////////////////////////////////////////////////////////////////////////////

// SetSource binds a ScriptSource on an acquired Engine.
// Note: the binding is LOCAL to that Engine instance; other engines in the pool
// are unaffected. See the package-level note above for pool-wide setup.
func (p *EnginePool) SetSource(source source.Reader) {
	eng, err := p.Acquire()
	if err != nil {
		return
	}
	defer p.Release(eng)
	eng.SetSource(source)
}

// GetSource returns the ScriptSource bound to an acquired Engine (or nil).
func (p *EnginePool) GetSource() source.Reader {
	eng, err := p.Acquire()
	if err != nil {
		return nil
	}
	defer p.Release(eng)
	return eng.GetSource()
}

////////////////////////////////////////////////////////////////////////////////
// Script loading
////////////////////////////////////////////////////////////////////////////////

// Load loads a single script from the bound Source into an acquired Engine.
func (p *EnginePool) Load(ctx context.Context, key string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.Load(ctx, key)
}

// LoadMulti loads multiple scripts from the bound Source into an acquired Engine.
func (p *EnginePool) LoadMulti(ctx context.Context, keys []string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadMulti(ctx, keys)
}

// LoadString compiles an inline script (name+code) on an acquired Engine.
// It does NOT go through the bound Source.
func (p *EnginePool) LoadString(ctx context.Context, name string, code string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadString(ctx, name, code)
}

////////////////////////////////////////////////////////////////////////////////
// Script execution
////////////////////////////////////////////////////////////////////////////////

// Execute runs every previously-loaded script on an acquired Engine and returns
// the combined result.
func (p *EnginePool) Execute(ctx context.Context) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.Execute(ctx)
}

// ExecuteFromKey loads (via the bound Source) and immediately runs the script
// identified by key on an acquired Engine.
func (p *EnginePool) ExecuteFromKey(ctx context.Context, key string) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteFromKey(ctx, key)
}

// ExecuteFromKeys is the multi-key variant of ExecuteFromKey; results are
// returned in the same order as keys.
func (p *EnginePool) ExecuteFromKeys(ctx context.Context, keys []string) ([]any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteFromKeys(ctx, keys)
}

// ExecuteString compiles and immediately runs the inline script (name+code) on
// an acquired Engine, bypassing the bound Source.
func (p *EnginePool) ExecuteString(ctx context.Context, name string, code string) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteString(ctx, name, code)
}

////////////////////////////////////////////////////////////////////////////////
// Globals, functions, modules
////////////////////////////////////////////////////////////////////////////////

// RegisterGlobal registers a global variable on an acquired Engine.
// Note: the registration is LOCAL to that Engine instance.
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

// RegisterFunction registers a host function on an acquired Engine.
// Note: the registration is LOCAL to that Engine instance.
func (p *EnginePool) RegisterFunction(name string, fn any) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.RegisterFunction(name, fn)
}

// CallFunction invokes the named script function on an acquired Engine.
func (p *EnginePool) CallFunction(ctx context.Context, name string, args ...any) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.CallFunction(ctx, name, args...)
}

// RegisterModule registers a module on an acquired Engine.
// Note: the registration is LOCAL to that Engine instance.
func (p *EnginePool) RegisterModule(name string, module any) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.RegisterModule(name, module)
}

////////////////////////////////////////////////////////////////////////////////
// Hot Reload (Watch)
////////////////////////////////////////////////////////////////////////////////

// StartWatch starts watching the script identified by `key` on an acquired Engine.
// Note: the watch is LOCAL to that Engine instance.
func (p *EnginePool) StartWatch(ctx context.Context, key string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.StartWatch(ctx, key)
}

// StopWatch stops watching the script identified by `key` on an acquired Engine.
func (p *EnginePool) StopWatch(key string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.StopWatch(key)
}

////////////////////////////////////////////////////////////////////////////////
// Error handling
////////////////////////////////////////////////////////////////////////////////

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
