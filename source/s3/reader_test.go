package s3

import (
	"context"
	"encoding/base64"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tx7do/go-scripts/source"
)

// crc32cTable is the Castagnoli table used by AWS S3 for x-amz-checksum-crc32c.
var crc32cTable = crc32.MakeTable(crc32.Castagnoli)

// fakeS3 is a minimal S3-compatible HTTP server for testing Source.
// It ignores signatures and serves GET / HEAD against an in-memory map.
//
// The handler speaks path-style addressing (https://endpoint/<bucket>/<key>)
// so it must be combined with WithPathStyle() on the client side.
type fakeS3 struct {
	tb     testing.TB
	server *httptest.Server

	mu      sync.Mutex
	objects map[string]fakeObject

	getCount  atomic.Int64
	headCount atomic.Int64
}

// fakeObject is the in-memory record served by fakeS3.
type fakeObject struct {
	body     string
	etag     string
	modified time.Time
}

// newFakeS3 spins up a fake server and registers its Close on tb.Cleanup.
func newFakeS3(tb testing.TB, bucket string) *fakeS3 {
	tb.Helper()
	f := &fakeS3{
		tb:      tb,
		objects: make(map[string]fakeObject),
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.handler(bucket, w, r)
	}))
	tb.Cleanup(f.server.Close)
	return f
}

// set inserts or overwrites an object in the fake server.
func (f *fakeS3) set(key, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = fakeObject{
		body:     body,
		etag:     "\"" + tinyHash(body) + "\"",
		modified: time.Now(),
	}
}

// setEtag is like set but with an explicit ETag (useful for testing the
// hot-reload comparator without relying on body content).
func (f *fakeS3) setEtag(key, body, etag string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	prev := f.objects[key]
	prev.body = body
	prev.etag = etag
	prev.modified = time.Now()
	f.objects[key] = prev
}

// handler implements the path-style S3 protocol subset: GET and HEAD against
// /<bucket>/<key>.
func (f *fakeS3) handler(bucket string, w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
	if len(parts) != 2 || parts[0] != bucket {
		http.Error(w, "unknown bucket", http.StatusNotFound)
		return
	}
	key := parts[1]

	f.mu.Lock()
	obj, ok := f.objects[key]
	f.mu.Unlock()

	if !ok {
		http.Error(w, "NoSuchKey", http.StatusNotFound)
		return
	}

	w.Header().Set("ETag", obj.etag)
	w.Header().Set("Last-Modified", obj.modified.UTC().Format(http.TimeFormat))
	// Emit an S3-style CRC32C checksum so the SDK doesn't complain about a
	// missing checksum. CRC32C is the SDK v2 default for S3.
	checksum := crc32.Checksum([]byte(obj.body), crc32cTable)
	sumBuf := make([]byte, 4)
	for i := 0; i < 4; i++ {
		sumBuf[i] = byte(checksum >> (24 - i*8))
	}
	w.Header().Set("x-amz-checksum-crc32c", base64.StdEncoding.EncodeToString(sumBuf))

	switch r.Method {
	case http.MethodGet:
		f.getCount.Add(1)
		_, _ = io.WriteString(w, obj.body)
	case http.MethodHead:
		f.headCount.Add(1)
		// Body is intentionally empty for HEAD per HTTP spec.
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// tinyHash produces a content-derived token for ETag. It is NOT a real hash;
// it just needs to change when content changes so tests can verify hot-reload
// behavior. We avoid crypto/md5 to keep the test binary lean.
func tinyHash(s string) string {
	// Replace spaces to produce a stable, content-dependent token.
	return strings.ReplaceAll(s, " ", "_")
}

// newTestSource builds a Reader pointing at the fake server with anonymous
// credentials and path-style addressing. Additional opts are appended after
// the defaults so callers can override (e.g. with WithPrefix).
func newTestSource(t *testing.T, f *fakeS3, bucket string, opts ...Option) *Reader {
	t.Helper()
	ctx := context.Background()
	defaults := []Option{
		WithEndpoint(f.server.URL),
		WithPathStyle(),
		WithRegion("us-east-1"),
		WithAnonymous(), // skip signing for the fake server
	}
	all := append(defaults, opts...)
	src, err := New(ctx, bucket, all...)
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

// TestNew_RequiresBucket verifies that New rejects an empty bucket name.
func TestNew_RequiresBucket(t *testing.T) {
	_, err := New(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bucket")
}

// TestWithPrefix_Normalized verifies the prefix normalization rules.
func TestWithPrefix_Normalized(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"/", ""},
		{"scripts", "scripts/"},
		{"/scripts", "scripts/"},
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

// TestLoad_HappyPath verifies the basic Load flow against an HTTP fake.
func TestLoad_HappyPath(t *testing.T) {
	f := newFakeS3(t, "test-bucket")
	f.set("hello.lua", "print('hi')")

	src := newTestSource(t, f, "test-bucket")
	defer src.Close()

	code, err := src.Load(context.Background(), "hello.lua")
	require.NoError(t, err)
	assert.Equal(t, "print('hi')", code)
	assert.Equal(t, int64(1), f.getCount.Load())
}

// TestLoad_NotFound_WrapsSentinel verifies that a missing object surfaces as
// ErrNotFound so callers can use errors.Is.
func TestLoad_NotFound_WrapsSentinel(t *testing.T) {
	f := newFakeS3(t, "test-bucket")
	src := newTestSource(t, f, "test-bucket")
	defer src.Close()

	_, err := src.Load(context.Background(), "missing.lua")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound), "want ErrNotFound, got %v", err)
	assert.True(t, IsNotFound(err))
}

// TestLoad_WithPrefix verifies that prefix is prepended transparently.
func TestLoad_WithPrefix(t *testing.T) {
	f := newFakeS3(t, "test-bucket")
	f.set("scripts/lua/main.lua", "return 1")

	src := newTestSource(t, f, "test-bucket", WithPrefix("scripts/lua"))
	defer src.Close()

	code, err := src.Load(context.Background(), "main.lua")
	require.NoError(t, err)
	assert.Equal(t, "return 1", code)
}

// TestLoad_ContextCanceled verifies that a pre-canceled context short-circuits
// the call.
func TestLoad_ContextCanceled(t *testing.T) {
	f := newFakeS3(t, "test-bucket")
	src := newTestSource(t, f, "test-bucket")
	defer src.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := src.Load(ctx, "any")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// TestNew_WithStaticCredentials verifies that the static-credentials option
// path doesn't break New and that the fake server (which ignores signatures)
// still responds correctly.
func TestNew_WithStaticCredentials(t *testing.T) {
	f := newFakeS3(t, "test-bucket")
	f.set("k", "v")

	ctx := context.Background()
	src, err := New(ctx, "test-bucket",
		WithEndpoint(f.server.URL),
		WithPathStyle(),
		WithRegion("us-east-1"),
		WithStaticCredentials("AK", "SK", ""),
	)
	require.NoError(t, err)
	defer src.Close()

	code, err := src.Load(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, "v", code)
}

// TestClose_NoError verifies that Close succeeds on a fresh Source.
func TestClose_NoError(t *testing.T) {
	f := newFakeS3(t, "test-bucket")
	src := newTestSource(t, f, "test-bucket")
	assert.NoError(t, src.Close())
}

// TestLoad_Concurrent verifies that concurrent Loads against the same Source
// are safe and all observe the expected content.
func TestLoad_Concurrent(t *testing.T) {
	f := newFakeS3(t, "test-bucket")
	f.set("shared.lua", "-- shared body\n")

	src := newTestSource(t, f, "test-bucket")
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
	assert.Equal(t, int64(goroutines), f.getCount.Load())
}

// TestWatch_ObjectModified verifies that Watch signals when an object's ETag changes.
func TestWatch_ObjectModified(t *testing.T) {
	f := newFakeS3(t, "test-bucket")
	f.setEtag("script.lua", "v1", `"etag-v1"`)

	src := newTestSource(t, f, "test-bucket")
	defer src.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	ch, err := src.Watch(ctx, "script.lua")
	require.NoError(t, err)

	// Wait for initial HEAD request to complete, then modify the object.
	time.Sleep(200 * time.Millisecond)
	f.setEtag("script.lua", "v2", `"etag-v2"`)

	// Polling interval is 5s, so we need to wait a bit longer.
	select {
	case <-ch:
		// Signal received as expected.
	case <-ctx.Done():
		t.Fatal("timeout waiting for watch signal")
	}
}

// TestWatch_ContextCancelled verifies that Watch closes the channel on context cancellation.
func TestWatch_ContextCancelled(t *testing.T) {
	f := newFakeS3(t, "test-bucket")
	f.set("script.lua", "v1")

	src := newTestSource(t, f, "test-bucket")
	defer src.Close()

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

// TestWatch_MissingObject verifies that Watch returns an error for non-existent objects.
func TestWatch_MissingObject(t *testing.T) {
	f := newFakeS3(t, "test-bucket")

	src := newTestSource(t, f, "test-bucket")
	defer src.Close()

	_, err := src.Watch(context.Background(), "missing.lua")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "head object")
}
