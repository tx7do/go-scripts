package script_engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tx7do/go-scripts/source"
)

////////////////////////////////////////////////////////////////////////////////
// EnginePool - construction & validation
////////////////////////////////////////////////////////////////////////////////

const poolTestType Type = "mock-pool"

// withPoolFactory registers a mock factory under poolTestType and returns a
// cleanup. Every engine instance created during the test is recorded on the
// returned counter for leak checks.
func withPoolFactory(t *testing.T, initErr, closeErr error) (*int64, func()) {
	t.Helper()
	Unregister(poolTestType)
	var created int64
	require.NoError(t, Register(poolTestType, func() (Engine, error) {
		m := newMockEngine(poolTestType)
		m.initErr = initErr
		m.closeErr = closeErr
		atomic.AddInt64(&created, 1)
		return m, nil
	}))
	return &created, func() { Unregister(poolTestType) }
}

func TestNewEnginePool_InvalidSize(t *testing.T) {
	_, cleanup := withPoolFactory(t, nil, nil)
	defer cleanup()

	_, err := NewEnginePool(0, poolTestType)
	require.Error(t, err)

	_, err = NewEnginePool(-1, poolTestType)
	require.Error(t, err)
}

func TestNewEnginePool_FactoryError(t *testing.T) {
	// Register a factory that always fails, then ensure NewEnginePool bubbles
	// the error up and does not leak resources.
	Unregister(poolTestType)
	require.NoError(t, Register(poolTestType, func() (Engine, error) {
		return nil, errors.New("factory boom")
	}))
	defer Unregister(poolTestType)

	_, err := NewEnginePool(3, poolTestType)
	require.Error(t, err)
}

func TestNewEnginePool_InitError(t *testing.T) {
	// Inject an Init error; every engine created so far must be Closed by the
	// pool's cleanup path.
	Unregister(poolTestType)
	var createdMu sync.Mutex
	var createdEngines []*mockEngine
	require.NoError(t, Register(poolTestType, func() (Engine, error) {
		m := newMockEngine(poolTestType)
		m.initErr = errors.New("init boom")
		createdMu.Lock()
		createdEngines = append(createdEngines, m)
		createdMu.Unlock()
		return m, nil
	}))
	defer Unregister(poolTestType)

	_, err := NewEnginePool(2, poolTestType)
	require.Error(t, err)

	// The engine that failed to Init must have been Closed by the pool cleanup.
	createdMu.Lock()
	defer createdMu.Unlock()
	require.Len(t, createdEngines, 1)
	assert.Equal(t, 1, createdEngines[0].closeCount, "failed engine must be Closed")
}

////////////////////////////////////////////////////////////////////////////////
// EnginePool - Acquire / Release / Close
////////////////////////////////////////////////////////////////////////////////

func TestEnginePool_AcquireRelease(t *testing.T) {
	_, cleanup := withPoolFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewEnginePool(2, poolTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	a, err := pool.Acquire()
	require.NoError(t, err)
	assert.NotNil(t, a)

	pool.Release(a)
}

func TestEnginePool_Acquire_BlocksUntilReleased(t *testing.T) {
	_, cleanup := withPoolFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewEnginePool(1, poolTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	a, err := pool.Acquire()
	require.NoError(t, err)

	// Second Acquire on a size-1 pool should block.
	done := make(chan struct{})
	go func() {
		// Give the main goroutine a moment to enter the channel wait.
		time.Sleep(10 * time.Millisecond)
		pool.Release(a)
		close(done)
	}()

	b, err := pool.Acquire()
	require.NoError(t, err)
	assert.Same(t, a, b)
	<-done
}

func TestEnginePool_Acquire_AfterClose(t *testing.T) {
	_, cleanup := withPoolFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewEnginePool(1, poolTestType)
	require.NoError(t, err)

	require.NoError(t, pool.Close())
	_, err = pool.Acquire()
	require.Error(t, err)
}

func TestEnginePool_Release_NilEngine(t *testing.T) {
	_, cleanup := withPoolFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewEnginePool(1, poolTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	// Must not panic.
	pool.Release(nil)
}

func TestEnginePool_Release_AfterClose(t *testing.T) {
	_, cleanup := withPoolFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewEnginePool(1, poolTestType)
	require.NoError(t, err)

	eng, err := pool.Acquire()
	require.NoError(t, err)
	require.NoError(t, pool.Close())

	// Returning an engine to a closed pool must Close it instead of panicking.
	pool.Release(eng)
	mock := eng.(*mockEngine)
	assert.Equal(t, 1, mock.closeCount)
}

func TestEnginePool_Close_Twice(t *testing.T) {
	_, cleanup := withPoolFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewEnginePool(1, poolTestType)
	require.NoError(t, err)

	require.NoError(t, pool.Close())
	assert.NoError(t, pool.Close())
}

func TestEnginePool_IsClosed(t *testing.T) {
	_, cleanup := withPoolFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewEnginePool(1, poolTestType)
	require.NoError(t, err)
	assert.False(t, pool.IsClosed())

	require.NoError(t, pool.Close())
	assert.True(t, pool.IsClosed())
}

////////////////////////////////////////////////////////////////////////////////
// EnginePool - InitAll
////////////////////////////////////////////////////////////////////////////////

func TestEnginePool_InitAll(t *testing.T) {
	_, cleanup := withPoolFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewEnginePool(3, poolTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	// Mark engines as closed so InitAll will re-initialise them.
	for i := 0; i < 3; i++ {
		eng, acqErr := pool.Acquire()
		require.NoError(t, acqErr)
		mock := eng.(*mockEngine)
		mock.initialized = false
		pool.Release(eng)
	}

	require.NoError(t, pool.InitAll(context.Background()))
}

////////////////////////////////////////////////////////////////////////////////
// EnginePool - per-call wrappers (SetSource, Load, ExecuteFromKey, ...)
////////////////////////////////////////////////////////////////////////////////

func TestEnginePool_SetSource_GetSource(t *testing.T) {
	_, cleanup := withPoolFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewEnginePool(1, poolTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	want := source.NewMemSource()
	pool.SetSource(want)
	got := pool.GetSource()
	assert.Same(t, want, got)
}

func TestEnginePool_Load(t *testing.T) {
	_, cleanup := withPoolFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewEnginePool(1, poolTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	src := source.NewMemSource()
	src.Set("k", "code")
	pool.SetSource(src)

	require.NoError(t, pool.Load(context.Background(), "k"))
}

func TestEnginePool_LoadMulti(t *testing.T) {
	_, cleanup := withPoolFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewEnginePool(1, poolTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	src := source.NewMemSource()
	src.Set("a", "1")
	src.Set("b", "2")
	pool.SetSource(src)

	require.NoError(t, pool.LoadMulti(context.Background(), []string{"a", "b"}))
}

func TestEnginePool_ExecuteFromKey(t *testing.T) {
	_, cleanup := withPoolFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewEnginePool(1, poolTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	src := source.NewMemSource()
	src.Set("k", "code")
	pool.SetSource(src)

	got, err := pool.ExecuteFromKey(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, "exec-result", got)
}

func TestEnginePool_ExecuteFromKeys(t *testing.T) {
	_, cleanup := withPoolFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewEnginePool(1, poolTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	src := source.NewMemSource()
	src.Set("a", "1")
	src.Set("b", "2")
	pool.SetSource(src)

	got, err := pool.ExecuteFromKeys(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestEnginePool_RegisterGlobal_GetGlobal(t *testing.T) {
	_, cleanup := withPoolFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewEnginePool(1, poolTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	require.NoError(t, pool.RegisterGlobal("g", 42))
	got, err := pool.GetGlobal("g")
	require.NoError(t, err)
	assert.Equal(t, 42, got)
}

func TestEnginePool_RegisterModule(t *testing.T) {
	_, cleanup := withPoolFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewEnginePool(1, poolTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	require.NoError(t, pool.RegisterModule("m", map[string]any{"k": "v"}))
}

func TestEnginePool_CallFunction(t *testing.T) {
	_, cleanup := withPoolFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewEnginePool(1, poolTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	// Pre-set the mock's call result so CallFunction returns it.
	eng, err := pool.Acquire()
	require.NoError(t, err)
	mock := eng.(*mockEngine)
	mock.callResult = "hello"
	pool.Release(eng)

	got, err := pool.CallFunction(context.Background(), "add", 1, 2)
	require.NoError(t, err)
	assert.Equal(t, "hello", got)
}

////////////////////////////////////////////////////////////////////////////////
// EnginePool - concurrency
////////////////////////////////////////////////////////////////////////////////

func TestEnginePool_ConcurrentAcquireRelease(t *testing.T) {
	_, cleanup := withPoolFactory(t, nil, nil)
	defer cleanup()

	const size = 4
	pool, err := NewEnginePool(size, poolTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	const goroutines = 50
	const loops = 100
	var wg sync.WaitGroup
	var errCount int64
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < loops; j++ {
				eng, acqErr := pool.Acquire()
				if acqErr != nil {
					atomic.AddInt64(&errCount, 1)
					continue
				}
				// Do a tiny amount of "work".
				_ = eng.GetType()
				pool.Release(eng)
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, int64(0), atomic.LoadInt64(&errCount))
}
