package script_engine

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/tx7do/go-scripts/source"
)

// EngineFactoryFunc is the factory signature used by
// [NewAutoGrowEnginePoolWithFactory] to create a fully-initialized Engine
// instance. The factory owns BOTH construction and Init, so it can also apply
// per-instance configuration that must happen before Init — for example a Lua
// engine's sandbox allow-list (SetOpenLibs) or injecting global functions.
//
// The returned Engine MUST already be Init'd; the pool does not call Init.
type EngineFactoryFunc func(ctx context.Context) (Engine, error)

// AutoGrowEnginePool is an Engine pool that can grow on demand up to a configured
// maximum. Idle instances are reused; when none is available and the cap has not
// been reached, a new Engine is created on the fly.
//
// Instances are produced by factory; when the pool is built via
// [NewAutoGrowEnginePool] (the type-based constructor), factory wraps the
// registered factory for typ and performs Init internally, preserving the
// historical behavior.
type AutoGrowEnginePool struct {
	pool chan Engine

	// factory creates a fully-initialized Engine instance. It is invoked both
	// for eager pre-allocation and for lazy growth. Always non-nil.
	factory EngineFactoryFunc

	// typ is retained only for diagnostics (e.g. closed-pool error messages);
	// instance creation goes exclusively through factory.
	typ Type

	mu     sync.Mutex
	total  int // number of Engine instances currently alive
	max    int
	closed bool
}

// NewAutoGrowEnginePoolWithFactory creates a pool that grows on demand, where
// every Engine instance is produced by factory. factory controls construction,
// any pre-Init configuration (e.g. SetOpenLibs sandbox, RegisterGlobal), and
// Init itself; the returned Engine MUST be ready to use.
//
//   - initialSize: number of Engines to create eagerly (>= 0)
//   - maxSize: upper bound on instance count (must be >= initialSize and >= 1)
//   - factory: produces fully-initialized Engines; must not be nil
func NewAutoGrowEnginePoolWithFactory(initialSize, maxSize int, factory EngineFactoryFunc) (*AutoGrowEnginePool, error) {
	if maxSize < 1 || initialSize < 0 || initialSize > maxSize {
		return nil, fmt.Errorf("invalid sizes: initial=%d max=%d", initialSize, maxSize)
	}
	if factory == nil {
		return nil, errors.New("script engine: factory cannot be nil")
	}

	p := &AutoGrowEnginePool{
		pool:    make(chan Engine, maxSize), // channel capacity equals the cap
		factory: factory,
		total:   0,
		max:     maxSize,
	}

	ctx := context.Background()

	// Build the initial set of Engines; on any failure clean them all up.
	created := make([]Engine, 0, initialSize)
	for i := 0; i < initialSize; i++ {
		eng, err := p.factory(ctx)
		if err != nil {
			for _, e := range created {
				_ = e.Close()
			}
			return nil, fmt.Errorf("script engine: factory failed: %w", err)
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

// NewAutoGrowEnginePool creates a pool that can grow on demand, using the
// factory registered for typ to build each Engine (and performing Init
// internally). It is a convenience wrapper around
// [NewAutoGrowEnginePoolWithFactory] for the common case where no per-instance
// pre-Init configuration is needed.
//
//   - initialSize: number of Engines to create eagerly (>= 0)
//   - maxSize: upper bound on instance count (must be >= initialSize and >= 1)
//   - typ: selects the registered factory used to build each Engine
func NewAutoGrowEnginePool(initialSize, maxSize int, typ Type) (*AutoGrowEnginePool, error) {
	if typ == "" {
		// Preserve the historical explicit check so the error reads the same.
		return nil, errors.New("engine type cannot be empty")
	}
	// The type may legitimately not be registered yet; surface that as a
	// factory failure (consistent with the original behavior) rather than here.
	factory := func(ctx context.Context) (Engine, error) {
		eng, err := NewScriptEngine(typ)
		if err != nil {
			return nil, err
		}
		if initErr := eng.Init(ctx); initErr != nil {
			_ = eng.Close()
			return nil, initErr
		}
		return eng, nil
	}

	p, err := NewAutoGrowEnginePoolWithFactory(initialSize, maxSize, factory)
	if err != nil {
		return nil, err
	}
	p.typ = typ
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
		eng, err := p.factory(context.Background())
		if err != nil {
			// Creation failed; roll back the counter.
			p.mu.Lock()
			p.total--
			p.mu.Unlock()
			return nil, err
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

////////////////////////////////////////////////////////////////////////////////
// ScriptSource injection & access
////////////////////////////////////////////////////////////////////////////////

// SetSource binds a ScriptSource on an acquired Engine.
// Note: the binding is LOCAL to that Engine instance; other engines in the pool
// are unaffected. See the package-level note above for pool-wide setup.
func (p *AutoGrowEnginePool) SetSource(source source.Reader) {
	eng, err := p.Acquire()
	if err != nil {
		return
	}
	defer p.Release(eng)
	eng.SetSource(source)
}

// GetSource returns the ScriptSource bound to an acquired Engine (or nil).
func (p *AutoGrowEnginePool) GetSource() source.Reader {
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
func (p *AutoGrowEnginePool) Load(ctx context.Context, key string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.Load(ctx, key)
}

// LoadMulti loads multiple scripts from the bound Source into an acquired Engine.
func (p *AutoGrowEnginePool) LoadMulti(ctx context.Context, keys []string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.LoadMulti(ctx, keys)
}

// LoadString compiles an inline script (name+code) on an acquired Engine.
// It does NOT go through the bound Source.
func (p *AutoGrowEnginePool) LoadString(ctx context.Context, name string, code string) error {
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
func (p *AutoGrowEnginePool) Execute(ctx context.Context) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.Execute(ctx)
}

// ExecuteFromKey loads (via the bound Source) and immediately runs the script
// identified by key on an acquired Engine.
func (p *AutoGrowEnginePool) ExecuteFromKey(ctx context.Context, key string) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteFromKey(ctx, key)
}

// ExecuteFromKeys is the multi-key variant of ExecuteFromKey; results are
// returned in the same order as keys.
func (p *AutoGrowEnginePool) ExecuteFromKeys(ctx context.Context, keys []string) ([]any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.ExecuteFromKeys(ctx, keys)
}

// ExecuteString compiles and immediately runs the inline script (name+code) on
// an acquired Engine, bypassing the bound Source.
func (p *AutoGrowEnginePool) ExecuteString(ctx context.Context, name string, code string) (any, error) {
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

// RegisterFunction registers a host function on an acquired Engine.
// Note: the registration is LOCAL to that Engine instance.
func (p *AutoGrowEnginePool) RegisterFunction(name string, fn any) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.RegisterFunction(name, fn)
}

// CallFunction invokes the named script function on an acquired Engine.
func (p *AutoGrowEnginePool) CallFunction(ctx context.Context, name string, args ...any) (any, error) {
	eng, err := p.Acquire()
	if err != nil {
		return nil, err
	}
	defer p.Release(eng)
	return eng.CallFunction(ctx, name, args...)
}

// RegisterModule registers a module on an acquired Engine.
// Note: the registration is LOCAL to that Engine instance.
func (p *AutoGrowEnginePool) RegisterModule(name string, module any) error {
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
func (p *AutoGrowEnginePool) StartWatch(ctx context.Context, key string) error {
	eng, err := p.Acquire()
	if err != nil {
		return err
	}
	defer p.Release(eng)
	return eng.StartWatch(ctx, key)
}

// StopWatch stops watching the script identified by `key` on an acquired Engine.
func (p *AutoGrowEnginePool) StopWatch(key string) error {
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
