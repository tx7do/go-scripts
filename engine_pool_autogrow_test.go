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
)

const autogrowTestType Type = "mock-autogrow"

// withAutoGrowFactory registers a mock factory under autogrowTestType and
// returns a cleanup func. Every created engine is appended to *created so the
// test can count live instances.
func withAutoGrowFactory(t *testing.T, initErr, closeErr error) (*[]*mockEngine, *sync.Mutex, func()) {
	t.Helper()
	Unregister(autogrowTestType)
	var mu sync.Mutex
	var created []*mockEngine
	require.NoError(t, Register(autogrowTestType, func() (Engine, error) {
		m := newMockEngine(autogrowTestType)
		m.initErr = initErr
		m.closeErr = closeErr
		mu.Lock()
		created = append(created, m)
		mu.Unlock()
		return m, nil
	}))
	return &created, &mu, func() { Unregister(autogrowTestType) }
}

////////////////////////////////////////////////////////////////////////////////
// AutoGrowEnginePool - construction & validation
////////////////////////////////////////////////////////////////////////////////

func TestNewAutoGrowEnginePool_InvalidSizes(t *testing.T) {
	_, _, cleanup := withAutoGrowFactory(t, nil, nil)
	defer cleanup()

	// maxSize < 1
	_, err := NewAutoGrowEnginePool(0, 0, autogrowTestType)
	require.Error(t, err)

	// initialSize < 0
	_, err = NewAutoGrowEnginePool(-1, 1, autogrowTestType)
	require.Error(t, err)

	// initialSize > maxSize
	_, err = NewAutoGrowEnginePool(2, 1, autogrowTestType)
	require.Error(t, err)
}

func TestNewAutoGrowEnginePool_EmptyType(t *testing.T) {
	_, _, cleanup := withAutoGrowFactory(t, nil, nil)
	defer cleanup()

	_, err := NewAutoGrowEnginePool(0, 1, "")
	require.Error(t, err)
}

func TestNewAutoGrowEnginePool_FactoryError(t *testing.T) {
	Unregister(autogrowTestType)
	require.NoError(t, Register(autogrowTestType, func() (Engine, error) {
		return nil, errors.New("factory boom")
	}))
	defer Unregister(autogrowTestType)

	_, err := NewAutoGrowEnginePool(1, 1, autogrowTestType)
	require.Error(t, err)
}

func TestNewAutoGrowEnginePool_InitError(t *testing.T) {
	Unregister(autogrowTestType)
	var mu sync.Mutex
	var created []*mockEngine
	require.NoError(t, Register(autogrowTestType, func() (Engine, error) {
		m := newMockEngine(autogrowTestType)
		m.initErr = errors.New("init boom")
		mu.Lock()
		created = append(created, m)
		mu.Unlock()
		return m, nil
	}))
	defer Unregister(autogrowTestType)

	_, err := NewAutoGrowEnginePool(2, 2, autogrowTestType)
	require.Error(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, created, 1)
	assert.Equal(t, 1, created[0].closeCount, "failed engine must be Closed")
}

func TestNewAutoGrowEnginePool_ZeroInitial(t *testing.T) {
	// initialSize=0 is valid: pool starts empty and grows on demand.
	_, _, cleanup := withAutoGrowFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewAutoGrowEnginePool(0, 2, autogrowTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	eng, err := pool.Acquire()
	require.NoError(t, err)
	assert.NotNil(t, eng)
	pool.Release(eng)
}

////////////////////////////////////////////////////////////////////////////////
// AutoGrowEnginePool - Acquire / Release / grow
////////////////////////////////////////////////////////////////////////////////

func TestAutoGrowEnginePool_Acquire_GrowsUnderCap(t *testing.T) {
	created, mu, cleanup := withAutoGrowFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewAutoGrowEnginePool(0, 3, autogrowTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	// Three acquires without releases must create 3 engines (cap=3).
	a, err := pool.Acquire()
	require.NoError(t, err)
	b, err := pool.Acquire()
	require.NoError(t, err)
	c, err := pool.Acquire()
	require.NoError(t, err)
	assert.NotSame(t, a, b)
	assert.NotSame(t, a, c)

	mu.Lock()
	assert.Len(t, *created, 3)
	mu.Unlock()
}

func TestAutoGrowEnginePool_Acquire_BlocksAtCap(t *testing.T) {
	_, _, cleanup := withAutoGrowFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewAutoGrowEnginePool(1, 1, autogrowTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	a, err := pool.Acquire()
	require.NoError(t, err)

	// Second Acquire must block because cap is reached.
	done := make(chan struct{})
	go func() {
		time.Sleep(10 * time.Millisecond)
		pool.Release(a)
		close(done)
	}()

	b, err := pool.Acquire()
	require.NoError(t, err)
	assert.Same(t, a, b)
	<-done
}

func TestAutoGrowEnginePool_Release_AfterClose(t *testing.T) {
	_, _, cleanup := withAutoGrowFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewAutoGrowEnginePool(1, 1, autogrowTestType)
	require.NoError(t, err)

	eng, err := pool.Acquire()
	require.NoError(t, err)
	mock := eng.(*mockEngine)

	require.NoError(t, pool.Close())
	pool.Release(eng)
	assert.Equal(t, 1, mock.closeCount, "borrowed engine must be Closed when pool is closed")
}

func TestAutoGrowEnginePool_Close_Twice(t *testing.T) {
	_, _, cleanup := withAutoGrowFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewAutoGrowEnginePool(0, 1, autogrowTestType)
	require.NoError(t, err)

	require.NoError(t, pool.Close())
	assert.NoError(t, pool.Close())
}

////////////////////////////////////////////////////////////////////////////////
// AutoGrowEnginePool - wrappers (a few key ones; the wrapper pattern is the
// same as the fixed pool's, so we don't duplicate every wrapper).
////////////////////////////////////////////////////////////////////////////////

func TestAutoGrowEnginePool_SetSource_Load(t *testing.T) {
	_, _, cleanup := withAutoGrowFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewAutoGrowEnginePool(0, 2, autogrowTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	src := NewMemSource()
	src.Set("k", "code")
	pool.SetSource(src)

	require.NoError(t, pool.Load(context.Background(), "k"))
}

func TestAutoGrowEnginePool_ExecuteFromKey(t *testing.T) {
	_, _, cleanup := withAutoGrowFactory(t, nil, nil)
	defer cleanup()

	pool, err := NewAutoGrowEnginePool(0, 2, autogrowTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	src := NewMemSource()
	src.Set("k", "code")
	pool.SetSource(src)

	got, err := pool.ExecuteFromKey(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, "exec-result", got)
}

////////////////////////////////////////////////////////////////////////////////
// AutoGrowEnginePool - concurrency
////////////////////////////////////////////////////////////////////////////////

func TestAutoGrowEnginePool_ConcurrentAcquireRelease(t *testing.T) {
	_, _, cleanup := withAutoGrowFactory(t, nil, nil)
	defer cleanup()

	const initial = 1
	const max = 4
	pool, err := NewAutoGrowEnginePool(initial, max, autogrowTestType)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	const goroutines = 30
	const loops = 50
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
				_ = eng.GetType()
				pool.Release(eng)
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, int64(0), atomic.LoadInt64(&errCount))
}
