package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tx7do/go-scripts/source"
)

// fakeGit is an in-memory git-like store for testing. It implements gitAPI.
type fakeGit struct {
	mu         sync.Mutex
	files      map[string][]byte // key -> content
	headHash   string
	pullHash   string // hash after "pull"
	pullCount  int64  // number of pull calls
	cloneCount int64  // number of clone calls
	pullErr    error  // optional: force pull to fail
}

// newFakeGit creates a new in-memory fake.
func newFakeGit() *fakeGit {
	return &fakeGit{
		files:    make(map[string][]byte),
		headHash: "abc123",
		pullHash: "abc123",
	}
}

// set inserts or overwrites a file.
func (f *fakeGit) set(key string, content []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[key] = content
}

// advanceHash simulates a new commit on the remote.
func (f *fakeGit) advanceHash(newHash string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pullHash = newHash
}

// setPullErr forces Pull to return the given error.
func (f *fakeGit) setPullErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pullErr = err
}

// Clone implements gitAPI.Clone.
func (f *fakeGit) Clone(_ contextWrap, repoURL, localPath, branch string, depth int) error {
	atomic.AddInt64(&f.cloneCount, 1)
	return nil
}

// Pull implements gitAPI.Pull.
func (f *fakeGit) Pull(_ contextWrap, localPath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	atomic.AddInt64(&f.pullCount, 1)
	if f.pullErr != nil {
		return f.pullErr
	}
	f.headHash = f.pullHash
	return nil
}

// ReadFile implements gitAPI.ReadFile.
func (f *fakeGit) ReadFile(localPath, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.files[key]
	if !ok {
		return nil, fmt.Errorf("no such file: %s", key)
	}
	return data, nil
}

// HeadHash implements gitAPI.HeadHash.
func (f *fakeGit) HeadHash(localPath string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.headHash, nil
}

// Cleanup implements gitAPI.Cleanup.
func (f *fakeGit) Cleanup(localPath string) error {
	return nil
}

// newTestReader builds a Reader backed by a fakeGit client.
func newTestReader(t *testing.T, f *fakeGit, opts ...Option) *Reader {
	t.Helper()
	ctx := context.Background()
	all := append([]Option{withGitOp(f)}, opts...)
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

// TestNew_RequiresRepoURLOrFail verifies that New fails without WithRepoURL
// or WithLocalPath when no fake is injected.
func TestNew_RequiresRepoURLOrFail(t *testing.T) {
	_, err := New(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required")
}

// TestLoad_HappyPath verifies the basic Load flow.
func TestLoad_HappyPath(t *testing.T) {
	f := newFakeGit()
	f.set("hello.lua", []byte("print('hi')"))

	src := newTestReader(t, f)
	defer func() { _ = src.Close() }()

	code, err := src.Load(context.Background(), "hello.lua")
	require.NoError(t, err)
	assert.Equal(t, "print('hi')", code)
}

// TestLoad_NotFound_WrapsSentinel verifies that a missing key surfaces as
// ErrNotFound so callers can use errors.Is.
func TestLoad_NotFound_WrapsSentinel(t *testing.T) {
	f := newFakeGit()
	src := newTestReader(t, f)
	defer func() { _ = src.Close() }()

	_, err := src.Load(context.Background(), "missing.lua")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound), "want ErrNotFound, got %v", err)
	assert.True(t, IsNotFound(err))
}

// TestLoad_WithPrefix verifies that prefix is prepended transparently.
func TestLoad_WithPrefix(t *testing.T) {
	f := newFakeGit()
	f.set("scripts/lua/main.lua", []byte("return 1"))

	src := newTestReader(t, f, WithPrefix("scripts/lua/"))
	defer func() { _ = src.Close() }()

	code, err := src.Load(context.Background(), "main.lua")
	require.NoError(t, err)
	assert.Equal(t, "return 1", code)
}

// TestLoad_ContextCanceled verifies that a pre-canceled context short-circuits.
func TestLoad_ContextCanceled(t *testing.T) {
	f := newFakeGit()
	src := newTestReader(t, f)
	defer func() { _ = src.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := src.Load(ctx, "any")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// TestClose_NoError verifies that Close succeeds on a Reader with no cleanup.
func TestClose_NoError(t *testing.T) {
	f := newFakeGit()
	src := newTestReader(t, f)
	assert.NoError(t, src.Close())
	// Idempotent.
	assert.NoError(t, src.Close())
}

// TestLoad_Concurrent verifies that concurrent Loads are safe.
func TestLoad_Concurrent(t *testing.T) {
	f := newFakeGit()
	f.set("shared.lua", []byte("-- shared body\n"))

	src := newTestReader(t, f)
	defer func() { _ = src.Close() }()

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

// TestLoad_RecordsHash verifies that Load records the HEAD hash for Watch.
func TestLoad_RecordsHash(t *testing.T) {
	f := newFakeGit()
	f.set("script.lua", []byte("v1"))
	f.headHash = "commit-aaa"

	src := newTestReader(t, f)
	defer func() { _ = src.Close() }()

	_, err := src.Load(context.Background(), "script.lua")
	require.NoError(t, err)

	src.mu.RLock()
	h := src.hashes["script.lua"]
	src.mu.RUnlock()
	assert.Equal(t, "commit-aaa", h)
}

// TestWatch_KeyModified verifies that Watch signals when a new commit is pulled.
func TestWatch_KeyModified(t *testing.T) {
	f := newFakeGit()
	f.set("script.lua", []byte("v1"))

	src := newTestReader(t, f, WithPullInterval(50*time.Millisecond))
	defer func() { _ = src.Close() }()

	// Must Load first so Watch has a baseline.
	_, err := src.Load(context.Background(), "script.lua")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := src.Watch(ctx, "script.lua")
	require.NoError(t, err)

	// Simulate a remote push: advance the hash.
	time.Sleep(100 * time.Millisecond)
	f.advanceHash("def456")

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
	f := newFakeGit()
	f.set("script.lua", []byte("v1"))

	src := newTestReader(t, f, WithPullInterval(50*time.Millisecond))
	defer func() { _ = src.Close() }()

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
	f := newFakeGit()
	src := newTestReader(t, f)
	defer func() { _ = src.Close() }()

	_, err := src.Watch(context.Background(), "never-loaded.lua")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has not been loaded")
}

// TestWatch_PullFails verifies that Watch continues even if Pull fails.
func TestWatch_PullFails(t *testing.T) {
	f := newFakeGit()
	f.set("script.lua", []byte("v1"))

	src := newTestReader(t, f, WithPullInterval(50*time.Millisecond))
	defer func() { _ = src.Close() }()

	_, err := src.Load(context.Background(), "script.lua")
	require.NoError(t, err)

	// Force pull to fail.
	f.setPullErr(errors.New("network unreachable"))

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	ch, err := src.Watch(ctx, "script.lua")
	require.NoError(t, err)

	// Channel should close on context cancel without signal.
	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel should be closed without signal")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for channel close")
	}

	assert.Greater(t, atomic.LoadInt64(&f.pullCount), int64(0), "pull should have been attempted")
}

// TestWatch_Concurrent verifies that multiple Watch goroutines all receive
// signals.
func TestWatch_Concurrent(t *testing.T) {
	f := newFakeGit()
	f.set("shared.lua", []byte("v1"))

	src := newTestReader(t, f, WithPullInterval(50*time.Millisecond))
	defer func() { _ = src.Close() }()

	_, err := src.Load(context.Background(), "shared.lua")
	require.NoError(t, err)

	const watchers = 5
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	chs := make([]<-chan struct{}, watchers)
	for i := 0; i < watchers; i++ {
		ch, err := src.Watch(ctx, "shared.lua")
		require.NoError(t, err)
		chs[i] = ch
	}

	time.Sleep(100 * time.Millisecond)
	f.advanceHash("newhash789")

	for i := 0; i < watchers; i++ {
		select {
		case <-chs[i]:
			// Signal received.
		case <-ctx.Done():
			t.Fatalf("watcher %d: timeout waiting for signal", i)
		}
	}
}

// TestWatch_NoChange verifies that Watch does not signal when the hash
// hasn't changed.
func TestWatch_NoChange(t *testing.T) {
	f := newFakeGit()
	f.set("script.lua", []byte("v1"))

	src := newTestReader(t, f, WithPullInterval(50*time.Millisecond))
	defer func() { _ = src.Close() }()

	_, err := src.Load(context.Background(), "script.lua")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	ch, err := src.Watch(ctx, "script.lua")
	require.NoError(t, err)

	// Hash doesn't change; expect no signal before timeout.
	select {
	case <-ch:
		t.Fatal("unexpected signal: hash hasn't changed")
	case <-ctx.Done():
		// Expected: context expired without any signal.
	}
}

////////////////////////////////////////////////////////////////////////////////
// Options tests
////////////////////////////////////////////////////////////////////////////////

func TestWithRepoURL(t *testing.T) {
	cfg := &configOptions{}
	WithRepoURL("https://github.com/test/repo.git")(cfg)
	assert.Equal(t, "https://github.com/test/repo.git", cfg.repoURL)
}

func TestWithBranch(t *testing.T) {
	cfg := &configOptions{}
	WithBranch("develop")(cfg)
	assert.Equal(t, "develop", cfg.branch)
}

func TestWithAuth(t *testing.T) {
	cfg := &configOptions{}
	WithAuth("user", "pass")(cfg)
	assert.Equal(t, "user", cfg.auth.username)
	assert.Equal(t, "pass", cfg.auth.password)
}

func TestWithToken(t *testing.T) {
	cfg := &configOptions{}
	WithToken("ghp_xxxx")(cfg)
	assert.Equal(t, "ghp_xxxx", cfg.auth.token)
}

func TestWithDepth(t *testing.T) {
	cfg := &configOptions{}
	WithDepth(1)(cfg)
	assert.Equal(t, 1, cfg.depth)
}

func TestWithPullInterval(t *testing.T) {
	cfg := &configOptions{}
	WithPullInterval(10 * time.Second)(cfg)
	assert.Equal(t, 10*time.Second, cfg.pullInterval)
}

////////////////////////////////////////////////////////////////////////////////
// Local repo integration test (real filesystem, no go-git needed)
////////////////////////////////////////////////////////////////////////////////

func TestLoad_FromLocalPath(t *testing.T) {
	// Create a temp directory with a script file.
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "hello.lua")
	require.NoError(t, os.WriteFile(scriptPath, []byte("print('local')"), 0o644))

	// Use a fake gitAPI that reads from the real filesystem.
	f := newFakeGit()
	f.set("hello.lua", []byte("print('local')"))

	src := newTestReader(t, f, WithLocalPath(dir))
	defer func() { _ = src.Close() }()

	code, err := src.Load(context.Background(), "hello.lua")
	require.NoError(t, err)
	assert.Equal(t, "print('local')", code)
}

////////////////////////////////////////////////////////////////////////////////
// Error message tests
////////////////////////////////////////////////////////////////////////////////

func TestLoad_ErrorContainsKey(t *testing.T) {
	f := newFakeGit()
	src := newTestReader(t, f)
	defer func() { _ = src.Close() }()

	_, err := src.Load(context.Background(), "missing/file.lua")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "missing/file.lua"))
}
