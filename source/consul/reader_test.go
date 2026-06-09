package consul

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tx7do/go-scripts/source"
)

// fakeConsul is an in-memory Consul KV store for testing.
// It implements consulAPI.
type fakeConsul struct {
	mu      sync.Mutex
	data    map[string]*api.KVPair
	getHits int64
	idx     uint64
}

// newFakeConsul creates a new in-memory fake.
func newFakeConsul() *fakeConsul {
	return &fakeConsul{
		data: make(map[string]*api.KVPair),
	}
}

// nextIndex returns a monotonically increasing Consul-style index.
func (f *fakeConsul) nextIndex() uint64 {
	f.idx++
	return f.idx
}

// set inserts or overwrites a key-value pair, bumping ModifyIndex.
func (f *fakeConsul) set(key, val string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	old, ok := f.data[key]
	newIdx := f.idx + 1
	f.idx = newIdx
	if ok {
		old.Value = []byte(val)
		old.ModifyIndex = newIdx
		f.data[key] = old
	} else {
		f.data[key] = &api.KVPair{
			Key:         key,
			Value:       []byte(val),
			CreateIndex: newIdx,
			ModifyIndex: newIdx,
		}
	}
}

// del removes a key (simulating Consul KV delete).
func (f *fakeConsul) del(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, key)
}

// Get implements consulAPI.
func (f *fakeConsul) Get(key string, _ *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	atomic.AddInt64(&f.getHits, 1)
	pair, ok := f.data[key]
	if !ok {
		return nil, &api.QueryMeta{}, nil
	}
	return pair, &api.QueryMeta{}, nil
}

// List implements consulAPI.
func (f *fakeConsul) List(prefix string, _ *api.QueryOptions) (api.KVPairs, *api.QueryMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out api.KVPairs
	for k, v := range f.data {
		if hasPrefix(k, prefix) {
			out = append(out, v)
		}
	}
	return out, &api.QueryMeta{}, nil
}

// hasPrefix is a simple string prefix check (kept inline to avoid importing
// strings when not needed elsewhere).
func hasPrefix(s, prefix string) bool {
	if len(prefix) == 0 {
		return true
	}
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}

// newTestReader builds a Reader backed by a fakeConsul client.
func newTestReader(t *testing.T, f *fakeConsul, opts ...Option) *Reader {
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
		{"scripts", "scripts"},
		{"/scripts", "scripts"},
		{"scripts/", "scripts/"},
		{"/scripts/", "scripts/"},
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

// TestLoad_HappyPath verifies the basic Load flow.
func TestLoad_HappyPath(t *testing.T) {
	f := newFakeConsul()
	f.set("hello.lua", "print('hi')")

	src := newTestReader(t, f)
	defer src.Close()

	code, err := src.Load(context.Background(), "hello.lua")
	require.NoError(t, err)
	assert.Equal(t, "print('hi')", code)
	assert.Equal(t, int64(1), atomic.LoadInt64(&f.getHits))
}

// TestLoad_NotFound_WrapsSentinel verifies that a missing key surfaces as
// ErrNotFound so callers can use errors.Is.
func TestLoad_NotFound_WrapsSentinel(t *testing.T) {
	f := newFakeConsul()
	src := newTestReader(t, f)
	defer src.Close()

	_, err := src.Load(context.Background(), "missing.lua")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound), "want ErrNotFound, got %v", err)
	assert.True(t, IsNotFound(err))
}

// TestLoad_WithPrefix verifies that prefix is prepended transparently.
func TestLoad_WithPrefix(t *testing.T) {
	f := newFakeConsul()
	f.set("scripts/lua/main.lua", "return 1")

	src := newTestReader(t, f, WithPrefix("scripts/lua/"))
	defer src.Close()

	code, err := src.Load(context.Background(), "main.lua")
	require.NoError(t, err)
	assert.Equal(t, "return 1", code)
}

// TestLoad_ContextCanceled verifies that a pre-canceled context short-circuits
// the call.
func TestLoad_ContextCanceled(t *testing.T) {
	f := newFakeConsul()
	src := newTestReader(t, f)
	defer src.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := src.Load(ctx, "any")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// TestClose_NoError verifies that Close succeeds on a Reader with no real closer.
func TestClose_NoError(t *testing.T) {
	f := newFakeConsul()
	src := newTestReader(t, f)
	assert.NoError(t, src.Close())
}

// TestLoad_Concurrent verifies that concurrent Loads are safe and all observe
// the expected content.
func TestLoad_Concurrent(t *testing.T) {
	f := newFakeConsul()
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
	assert.Equal(t, int64(goroutines), atomic.LoadInt64(&f.getHits))
}

// TestWatch_KeyModified verifies that Watch signals when a key's ModifyIndex
// changes.
//
// NOTE: Watch polls every 5 seconds. The test uses a generous timeout to
// accommodate the polling interval.
func TestWatch_KeyModified(t *testing.T) {
	f := newFakeConsul()
	f.set("script.lua", "v1")

	src := newTestReader(t, f)
	defer src.Close()

	// Must Load first so Watch has a baseline ModifyIndex.
	_, err := src.Load(context.Background(), "script.lua")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	ch, err := src.Watch(ctx, "script.lua")
	require.NoError(t, err)

	// Trigger a ModifyIndex change.
	time.Sleep(100 * time.Millisecond)
	f.set("script.lua", "v2")

	// Polling interval is 5s, so we need to wait a bit longer.
	select {
	case <-ch:
		// Signal received as expected.
	case <-ctx.Done():
		t.Fatal("timeout waiting for watch signal")
	}
}

// TestWatch_KeyDeleted verifies that Watch signals when a key is deleted.
func TestWatch_KeyDeleted(t *testing.T) {
	f := newFakeConsul()
	f.set("script.lua", "v1")

	src := newTestReader(t, f)
	defer src.Close()

	_, err := src.Load(context.Background(), "script.lua")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	ch, err := src.Watch(ctx, "script.lua")
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	f.del("script.lua")

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
	f := newFakeConsul()
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

// TestWatch_NotLoaded verifies that Watch returns an error if the key hasn't
// been loaded first.
func TestWatch_NotLoaded(t *testing.T) {
	f := newFakeConsul()
	src := newTestReader(t, f)
	defer src.Close()

	_, err := src.Watch(context.Background(), "never-loaded.lua")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has not been loaded")
}
