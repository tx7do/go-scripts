package http

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tx7do/go-scripts/source"
)

// fakeHTTP is a minimal HTTP test server for testing Source.
// It serves GET requests against an in-memory map of paths -> bodies.
type fakeHTTP struct {
	tb     testing.TB
	server *httptest.Server

	mu       sync.Mutex
	objects  map[string]string
	getCount atomic.Int64
}

// newFakeHTTP spins up a fake server and registers its Close on tb.Cleanup.
func newFakeHTTP(tb testing.TB) *fakeHTTP {
	tb.Helper()
	f := &fakeHTTP{
		tb:      tb,
		objects: make(map[string]string),
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handler))
	tb.Cleanup(f.server.Close)
	return f
}

// set inserts or overwrites a path -> body mapping.
func (f *fakeHTTP) set(path, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[path] = body
}

// handler serves GET requests against the in-memory map.
func (f *fakeHTTP) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	body, ok := f.objects[r.URL.Path]
	f.mu.Unlock()

	f.getCount.Add(1)

	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = io.WriteString(w, body)
}

// baseURL returns the server's base URL with a trailing slash.
func (f *fakeHTTP) baseURL() string {
	return f.server.URL + "/"
}

// newTestReader builds a Reader pointing at the fake server.
func newTestReader(t *testing.T, f *fakeHTTP, opts ...Option) *Reader {
	t.Helper()
	ctx := context.Background()
	defaults := []Option{WithBaseURL(f.baseURL())}
	all := append(defaults, opts...)
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

// TestNew_RequiresBaseURL verifies that New rejects an empty base URL.
func TestNew_RequiresBaseURL(t *testing.T) {
	_, err := New(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "base URL")
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

// TestLoad_HappyPath verifies the basic Load flow against an HTTP fake.
func TestLoad_HappyPath(t *testing.T) {
	f := newFakeHTTP(t)
	f.set("/hello.lua", "print('hi')")

	src := newTestReader(t, f)
	defer src.Close()

	code, err := src.Load(context.Background(), "hello.lua")
	require.NoError(t, err)
	assert.Equal(t, "print('hi')", code)
}

// TestLoad_NotFound_WrapsSentinel verifies that a 404 surfaces as ErrNotFound.
func TestLoad_NotFound_WrapsSentinel(t *testing.T) {
	f := newFakeHTTP(t)
	src := newTestReader(t, f)
	defer src.Close()

	_, err := src.Load(context.Background(), "missing.lua")
	require.Error(t, err)
	assert.True(t, IsNotFound(err), "want ErrNotFound, got %v", err)
}

// TestLoad_WithPrefix verifies that prefix is prepended transparently.
func TestLoad_WithPrefix(t *testing.T) {
	f := newFakeHTTP(t)
	f.set("/scripts/lua/main.lua", "return 1")

	src := newTestReader(t, f, WithPrefix("scripts/lua/"))
	defer src.Close()

	code, err := src.Load(context.Background(), "main.lua")
	require.NoError(t, err)
	assert.Equal(t, "return 1", code)
}

// TestLoad_WithHeader verifies custom headers are sent.
func TestLoad_WithHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	src, err := New(context.Background(),
		WithBaseURL(srv.URL+"/"),
		WithHeader("Authorization", "Bearer secret"),
	)
	require.NoError(t, err)
	defer src.Close()

	code, err := src.Load(context.Background(), "test.lua")
	require.NoError(t, err)
	assert.Equal(t, "ok", code)
	assert.Equal(t, "Bearer secret", gotAuth)
}

// TestLoad_ContextCanceled verifies that a pre-canceled context short-circuits.
func TestLoad_ContextCanceled(t *testing.T) {
	f := newFakeHTTP(t)
	src := newTestReader(t, f)
	defer src.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := src.Load(ctx, "any")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// TestClose_NoError verifies that Close succeeds.
func TestClose_NoError(t *testing.T) {
	f := newFakeHTTP(t)
	src := newTestReader(t, f)
	assert.NoError(t, src.Close())
}

// TestLoad_Concurrent verifies concurrent safety.
func TestLoad_Concurrent(t *testing.T) {
	f := newFakeHTTP(t)
	f.set("/shared.lua", "-- shared body\n")

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
}

////////////////////////////////////////////////////////////////////////////////
// Watch tests
////////////////////////////////////////////////////////////////////////////////

// TestWatch_ContentModified verifies that Watch signals when content changes.
func TestWatch_ContentModified(t *testing.T) {
	f := newFakeHTTP(t)
	f.set("/script.lua", "v1")

	src := newTestReader(t, f)
	defer src.Close()

	_, err := src.Load(context.Background(), "script.lua")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	ch, err := src.Watch(ctx, "script.lua")
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)
	f.set("/script.lua", "v2")

	select {
	case <-ch:
		// Signal received.
	case <-ctx.Done():
		t.Fatal("timeout waiting for watch signal")
	}
}

// TestWatch_ContextCancelled verifies channel closes on cancellation.
func TestWatch_ContextCancelled(t *testing.T) {
	f := newFakeHTTP(t)
	f.set("/script.lua", "v1")

	src := newTestReader(t, f)
	defer src.Close()

	_, err := src.Load(context.Background(), "script.lua")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := src.Watch(ctx, "script.lua")
	require.NoError(t, err)

	cancel()

	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel should be closed")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for channel close")
	}
}

// TestWatch_KeyNotLoaded verifies that Watch returns an error without Load first.
func TestWatch_KeyNotLoaded(t *testing.T) {
	f := newFakeHTTP(t)
	f.set("/script.lua", "v1")

	src := newTestReader(t, f)
	defer src.Close()

	_, err := src.Watch(context.Background(), "script.lua")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has not been loaded")
}

// TestLoad_ServerError verifies non-404 errors are reported.
func TestLoad_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal", http.StatusInternalServerError)
	}))
	defer srv.Close()

	src, err := New(context.Background(), WithBaseURL(srv.URL+"/"))
	require.NoError(t, err)
	defer src.Close()

	_, err = src.Load(context.Background(), "broken.lua")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

// Ensure errors is imported to avoid unused import if tests change.
var _ = errors.Is
