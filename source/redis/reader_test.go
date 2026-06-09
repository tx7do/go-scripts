package redis

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tx7do/go-scripts/source"
)

// fakeRedis is an in-memory Redis-like store for testing Source.
// It implements redisCmdable via fakeCmdable.
type fakeRedis struct {
	mu      sync.Mutex
	data    map[string]string
	getHits int64
	setHits int64
	delHits int64
}

// newFakeRedis creates a new in-memory fake.
func newFakeRedis() *fakeRedis {
	return &fakeRedis{data: make(map[string]string)}
}

// set inserts or overwrites a key-value pair in the fake store.
func (f *fakeRedis) set(key, val string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[key] = val
}

// del removes a key from the fake store.
func (f *fakeRedis) del(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, key)
}

// Get implements redisCmdable.
func (f *fakeRedis) Get(_ context.Context, key string) *redis.StringCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getHits++
	cmd := redis.NewStringCmd(context.Background())
	val, ok := f.data[key]
	if !ok {
		cmd.SetErr(redis.Nil)
	} else {
		cmd.SetVal(val)
	}
	return cmd
}

// Set implements redisCmdable.
func (f *fakeRedis) Set(_ context.Context, key string, value any, _ time.Duration) *redis.StatusCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setHits++
	f.data[key] = value.(string)
	cmd := redis.NewStatusCmd(context.Background())
	cmd.SetVal("OK")
	return cmd
}

// Del implements redisCmdable.
func (f *fakeRedis) Del(_ context.Context, keys ...string) *redis.IntCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delHits++
	var count int64
	for _, k := range keys {
		if _, ok := f.data[k]; ok {
			delete(f.data, k)
			count++
		}
	}
	cmd := redis.NewIntCmd(context.Background())
	cmd.SetVal(count)
	return cmd
}

// Ping implements redisCmdable.
func (f *fakeRedis) Ping(_ context.Context) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(context.Background())
	cmd.SetVal("PONG")
	return cmd
}

// newTestReader builds a Reader backed by a fakeRedis client.
func newTestReader(t *testing.T, f *fakeRedis, opts ...Option) *Reader {
	t.Helper()
	ctx := context.Background()
	all := append([]Option{withClient(f, nil)}, opts...)
	src, err := New(ctx, all...)
	require.NoError(t, err)
	return src
}

////////////////////////////////////////////////////////////////////////////////
// Tests
////////////////////////////////////////////////////////////////////////////////

// TestReader_ImplementsInterface is a compile-time guard.
func TestReader_ImplementsInterface(t *testing.T) {
	var _ source.Reader = (*Reader)(nil)
	var _ source.ReadWatcher = (*Reader)(nil)
}

// TestWithPrefix_Normalized verifies the prefix normalization rules.
func TestWithPrefix_Normalized(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"/", ""},
		{"scripts:", "scripts:"},
		{"/scripts:", "scripts:"},
		{"scripts/lua/", "scripts/lua/"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			cfg := &configOptions{}
			WithPrefix(tc.in)(cfg)
			assert.Equal(t, tc.want, cfg.prefix)
		})
	}
}

// TestLoad_HappyPath verifies the basic Load flow against an in-memory fake.
func TestLoad_HappyPath(t *testing.T) {
	f := newFakeRedis()
	f.set("hello.lua", "print('hi')")

	src := newTestReader(t, f)
	defer src.Close()

	code, err := src.Load(context.Background(), "hello.lua")
	require.NoError(t, err)
	assert.Equal(t, "print('hi')", code)
	assert.Equal(t, int64(1), f.getHits)
}

// TestLoad_NotFound_WrapsSentinel verifies that a missing key surfaces as
// ErrNotFound so callers can use errors.Is.
func TestLoad_NotFound_WrapsSentinel(t *testing.T) {
	f := newFakeRedis()
	src := newTestReader(t, f)
	defer src.Close()

	_, err := src.Load(context.Background(), "missing.lua")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound), "want ErrNotFound, got %v", err)
	assert.True(t, IsNotFound(err))
}

// TestLoad_WithPrefix verifies that prefix is prepended transparently.
func TestLoad_WithPrefix(t *testing.T) {
	f := newFakeRedis()
	f.set("scripts:lua:main.lua", "return 1")

	src := newTestReader(t, f, WithPrefix("scripts:lua:"))
	defer src.Close()

	code, err := src.Load(context.Background(), "main.lua")
	require.NoError(t, err)
	assert.Equal(t, "return 1", code)
}

// TestLoad_ContextCanceled verifies that a pre-canceled context short-circuits
// the call.
func TestLoad_ContextCanceled(t *testing.T) {
	f := newFakeRedis()
	src := newTestReader(t, f)
	defer src.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := src.Load(ctx, "any")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// TestClose_NoError verifies that Close succeeds on a fresh Source.
func TestClose_NoError(t *testing.T) {
	f := newFakeRedis()
	src := newTestReader(t, f)
	assert.NoError(t, src.Close())
}

// TestLoad_Concurrent verifies that concurrent Loads against the same Source
// are safe and all observe the expected content.
func TestLoad_Concurrent(t *testing.T) {
	f := newFakeRedis()
	f.set("shared.lua", "-- shared body\n")

	src := newTestReader(t, f)
	defer src.Close()

	const goroutines = 30
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			code, err := src.Load(context.Background(), "shared.lua")
			assert.NoError(t, err)
			assert.Equal(t, "-- shared body\n", code)
		}()
	}
	wg.Wait()
	assert.Equal(t, int64(goroutines), f.getHits)
}

////////////////////////////////////////////////////////////////////////////////
// Watch tests
////////////////////////////////////////////////////////////////////////////////

// TestWatch_ValueModified verifies that Watch signals when a key's value changes.
func TestWatch_ValueModified(t *testing.T) {
	f := newFakeRedis()
	f.set("script.lua", "v1")

	src := newTestReader(t, f)
	defer src.Close()

	// Load first so the initial value is tracked.
	_, err := src.Load(context.Background(), "script.lua")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	ch, err := src.Watch(ctx, "script.lua")
	require.NoError(t, err)

	// Modify the value after a short delay.
	time.Sleep(200 * time.Millisecond)
	f.set("script.lua", "v2")

	// Polling interval is 5s, so we need to wait a bit longer.
	select {
	case <-ch:
		// Signal received as expected.
	case <-ctx.Done():
		t.Fatal("timeout waiting for watch signal")
	}
}

// TestWatch_ContextCancelled verifies that Watch closes the channel on context
// cancellation.
func TestWatch_ContextCancelled(t *testing.T) {
	f := newFakeRedis()
	f.set("script.lua", "v1")

	src := newTestReader(t, f)
	defer src.Close()

	_, err := src.Load(context.Background(), "script.lua")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := src.Watch(ctx, "script.lua")
	require.NoError(t, err)

	// Cancel context immediately.
	cancel()

	// Channel should close quickly.
	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel should be closed")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for channel close")
	}
}

// TestWatch_KeyNotLoaded verifies that Watch returns an error if the key
// hasn't been loaded yet.
func TestWatch_KeyNotLoaded(t *testing.T) {
	f := newFakeRedis()
	f.set("script.lua", "v1")

	src := newTestReader(t, f)
	defer src.Close()

	// Do NOT call Load before Watch.
	_, err := src.Watch(context.Background(), "script.lua")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has not been loaded yet")
}
