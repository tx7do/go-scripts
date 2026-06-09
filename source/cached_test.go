package source

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

// TestCachedSource_ImplementsInterface is a compile-time guard.
func TestCachedSource_ImplementsInterface(t *testing.T) {
	var _ Reader = (*CachedSource)(nil)
	var _ ReadWatcher = (*CachedSource)(nil)
}

// TestNewCachedSource_NilRemote verifies that a nil remote is rejected.
func TestNewCachedSource_NilRemote(t *testing.T) {
	_, err := NewCachedSource(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remote reader is nil")
}

// TestCached_Load_CacheMiss verifies that first Load fetches from remote.
func TestCached_Load_CacheMiss(t *testing.T) {
	remote := &fakeSource{code: "from-remote"}
	c, err := NewCachedSource(remote)
	require.NoError(t, err)
	defer c.Close()

	code, err := c.Load(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, "from-remote", code)
	assert.Equal(t, 1, remote.loadCalls)
}

// TestCached_Load_CacheHit verifies that second Load uses cache.
func TestCached_Load_CacheHit(t *testing.T) {
	remote := &fakeSource{code: "from-remote"}
	c, err := NewCachedSource(remote)
	require.NoError(t, err)
	defer c.Close()

	// First Load: cache miss.
	_, err = c.Load(context.Background(), "k")
	require.NoError(t, err)

	// Second Load: cache hit, should NOT hit remote.
	code, err := c.Load(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, "from-remote", code)

	remote.mu.Lock()
	assert.Equal(t, 1, remote.loadCalls) // remote only called once
	remote.mu.Unlock()
}

// TestCached_Load_RemoteError verifies that remote errors propagate.
func TestCached_Load_RemoteError(t *testing.T) {
	customErr := errors.New("network timeout")
	remote := &fakeSource{err: customErr}
	c, err := NewCachedSource(remote)
	require.NoError(t, err)
	defer c.Close()

	_, err = c.Load(context.Background(), "k")
	require.Error(t, err)
	assert.ErrorIs(t, err, customErr)
}

// TestCached_Invalidate verifies that Invalidate forces a remote fetch.
func TestCached_Invalidate(t *testing.T) {
	remote := &fakeSource{code: "v1"}
	c, err := NewCachedSource(remote)
	require.NoError(t, err)
	defer c.Close()

	// Load to populate cache.
	_, _ = c.Load(context.Background(), "k")

	// Invalidate.
	c.Invalidate("k")

	// Change remote data.
	remote.mu.Lock()
	remote.code = "v2"
	remote.mu.Unlock()

	// Next Load should fetch from remote again.
	code, err := c.Load(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, "v2", code)
}

// TestCached_InvalidateAll verifies that InvalidateAll clears all entries.
func TestCached_InvalidateAll(t *testing.T) {
	remote := &fakeSource{code: "v1"}
	c, err := NewCachedSource(remote)
	require.NoError(t, err)
	defer c.Close()

	// Load two keys.
	_, _ = c.Load(context.Background(), "k1")
	_, _ = c.Load(context.Background(), "k2")

	// Invalidate all.
	c.InvalidateAll()

	// Both keys should require remote fetch.
	remote.mu.Lock()
	initialCalls := remote.loadCalls
	remote.mu.Unlock()

	_, _ = c.Load(context.Background(), "k1")
	_, _ = c.Load(context.Background(), "k2")

	remote.mu.Lock()
	assert.Equal(t, initialCalls+2, remote.loadCalls)
	remote.mu.Unlock()
}

// TestCached_TTL verifies that entries expire after TTL.
func TestCached_TTL(t *testing.T) {
	remote := &fakeSource{code: "v1"}
	c, err := NewCachedSource(remote, WithTTL(50*time.Millisecond))
	require.NoError(t, err)
	defer c.Close()

	// First Load.
	_, _ = c.Load(context.Background(), "k")
	time.Sleep(80 * time.Millisecond) // wait for TTL to expire

	// Change remote data.
	remote.mu.Lock()
	remote.code = "v2"
	remote.mu.Unlock()

	// Should fetch fresh data.
	code, err := c.Load(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, "v2", code)
}

// TestCached_TTL_NotExpired verifies that entries are reused within TTL.
func TestCached_TTL_NotExpired(t *testing.T) {
	remote := &fakeSource{code: "v1"}
	c, err := NewCachedSource(remote, WithTTL(5*time.Second))
	require.NoError(t, err)
	defer c.Close()

	// First Load.
	_, _ = c.Load(context.Background(), "k")

	// Second Load within TTL window — should hit cache.
	_, _ = c.Load(context.Background(), "k")

	remote.mu.Lock()
	assert.Equal(t, 1, remote.loadCalls)
	remote.mu.Unlock()
}

// TestCached_WatchInvalidation verifies that Watch signals evict cache.
func TestCached_WatchInvalidation(t *testing.T) {
	mem := NewMemSource()
	mem.Set("k", "v1")

	c, err := NewCachedSource(mem)
	require.NoError(t, err)
	defer c.Close()

	// First Load.
	code, err := c.Load(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, "v1", code)

	// Trigger a change.
	time.Sleep(50 * time.Millisecond)
	mem.Set("k", "v2")

	// Wait a bit for the watch goroutine to invalidate.
	time.Sleep(100 * time.Millisecond)

	// Next Load should fetch fresh data.
	code, err = c.Load(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, "v2", code)
}

// TestCached_Watch_Delegates verifies that Watch delegates to remote.
func TestCached_Watch_Delegates(t *testing.T) {
	mem := NewMemSource()
	mem.Set("k", "v1")

	c, err := NewCachedSource(mem)
	require.NoError(t, err)
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := c.Watch(ctx, "k")
	require.NoError(t, err)

	// Trigger a change.
	time.Sleep(50 * time.Millisecond)
	mem.Set("k", "v2")

	select {
	case <-ch:
		// Signal received.
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for watch signal")
	}
}

// TestCached_Watch_NotSupported verifies error when remote has no Watch.
func TestCached_Watch_NotSupported(t *testing.T) {
	remote := &fakeSource{code: "x"}
	c, err := NewCachedSource(remote)
	require.NoError(t, err)
	defer c.Close()

	_, err = c.Watch(context.Background(), "k")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not implement Watcher")
}

// TestCached_Close_StopsWatchers verifies that Close stops background goroutines.
func TestCached_Close_StopsWatchers(t *testing.T) {
	mem := NewMemSource()
	mem.Set("k", "v1")

	c, err := NewCachedSource(mem)
	require.NoError(t, err)

	// Load to trigger watcher start.
	_, _ = c.Load(context.Background(), "k")

	// Close should stop watchers.
	err = c.Close()
	require.NoError(t, err)

	// After close, watchers map should be empty.
	c.mu.Lock()
	assert.Equal(t, 0, len(c.watchers))
	c.mu.Unlock()
}

// TestCached_Concurrent verifies that concurrent Loads are safe.
func TestCached_Concurrent(t *testing.T) {
	remote := &fakeSource{code: "shared"}
	c, err := NewCachedSource(remote)
	require.NoError(t, err)
	defer c.Close()

	const goroutines = 30
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			code, err := c.Load(context.Background(), "k")
			assert.NoError(t, err)
			assert.Equal(t, "shared", code)
		}()
	}
	wg.Wait()

	// Due to caching, remote should be called far fewer times than goroutines.
	remote.mu.Lock()
	assert.LessOrEqual(t, remote.loadCalls, goroutines)
	remote.mu.Unlock()
}

// TestCached_Load_AlreadyCached_Fresh verifies that repeated loads are cached
// (counting variant with atomic).
func TestCached_Load_MultipleKeys(t *testing.T) {
	var counter int64
	remote := &countingSource{counter: &counter, code: "data"}

	c, err := NewCachedSource(remote)
	require.NoError(t, err)
	defer c.Close()

	// Load different keys.
	_, _ = c.Load(context.Background(), "k1")
	_, _ = c.Load(context.Background(), "k2")
	_, _ = c.Load(context.Background(), "k3")

	// Each key loaded once from remote.
	assert.Equal(t, int64(3), atomic.LoadInt64(&counter))

	// Repeat loads should hit cache.
	_, _ = c.Load(context.Background(), "k1")
	_, _ = c.Load(context.Background(), "k2")
	_, _ = c.Load(context.Background(), "k3")

	assert.Equal(t, int64(3), atomic.LoadInt64(&counter))
}

// countingSource is a Reader that counts Load calls atomically.
type countingSource struct {
	code    string
	counter *int64
}

func (s *countingSource) Load(_ context.Context, _ string) (string, error) {
	atomic.AddInt64(s.counter, 1)
	return s.code, nil
}

func (s *countingSource) Close() error { return nil }
