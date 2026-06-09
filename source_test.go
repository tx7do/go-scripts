package script_engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

////////////////////////////////////////////////////////////////////////////////
// FileSource
////////////////////////////////////////////////////////////////////////////////

// helper: create a temp file under t.TempDir() and return its absolute path.
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestFileSource_Load_OK(t *testing.T) {
	const want = "print('hello')"
	path := writeTempFile(t, "a.lua", want)

	src := NewFileSource()
	defer func() { _ = src.Close() }()

	got, err := src.Load(context.Background(), path)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestFileSource_Load_MissingFile(t *testing.T) {
	src := NewFileSource()
	defer func() { _ = src.Close() }()

	_, err := src.Load(context.Background(), filepath.Join(t.TempDir(), "missing.lua"))
	require.Error(t, err)
}

func TestFileSource_Load_CancelledContext(t *testing.T) {
	path := writeTempFile(t, "a.lua", "x")
	src := NewFileSource()
	defer func() { _ = src.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := src.Load(ctx, path)
	require.ErrorIs(t, err, context.Canceled)
}

func TestFileSource_ReloadCheck_NewFile(t *testing.T) {
	// A key that has never been Load'd must report changed=true.
	src := NewFileSource()
	defer func() { _ = src.Close() }()

	path := writeTempFile(t, "new.lua", "x")
	changed, err := src.ReloadCheck(context.Background(), path)
	require.NoError(t, err)
	assert.True(t, changed)
}

func TestFileSource_ReloadCheck_NoChange(t *testing.T) {
	path := writeTempFile(t, "stable.lua", "v1")
	src := NewFileSource()
	defer func() { _ = src.Close() }()

	_, err := src.Load(context.Background(), path)
	require.NoError(t, err)

	changed, err := src.ReloadCheck(context.Background(), path)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestFileSource_ReloadCheck_Modified(t *testing.T) {
	path := writeTempFile(t, "mod.lua", "v1")
	src := NewFileSource()
	defer func() { _ = src.Close() }()

	_, err := src.Load(context.Background(), path)
	require.NoError(t, err)

	// Wait a tiny bit so the next mtime is distinguishable, then rewrite the
	// file with an explicit mtime in the future.
	require.NoError(t, os.WriteFile(path, []byte("v2"), 0o644))
	future := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, future, future))

	changed, err := src.ReloadCheck(context.Background(), path)
	require.NoError(t, err)
	assert.True(t, changed)
}

func TestFileSource_ReloadCheck_StatError(t *testing.T) {
	// Missing file: ReloadCheck must surface an error.
	src := NewFileSource()
	defer func() { _ = src.Close() }()

	_, err := src.ReloadCheck(context.Background(), filepath.Join(t.TempDir(), "missing.lua"))
	require.Error(t, err)
}

func TestFileSource_Close_NoOp(t *testing.T) {
	src := NewFileSource()
	assert.NoError(t, src.Close())
	// Idempotent.
	assert.NoError(t, src.Close())
}

////////////////////////////////////////////////////////////////////////////////
// MemSource
////////////////////////////////////////////////////////////////////////////////

func TestMemSource_Load_NotFound(t *testing.T) {
	src := NewMemSource()
	defer func() { _ = src.Close() }()

	_, err := src.Load(context.Background(), "nope")
	require.Error(t, err)
}

func TestMemSource_SetAndLoad(t *testing.T) {
	src := NewMemSource()
	defer func() { _ = src.Close() }()

	src.Set("k", "return 1")
	got, err := src.Load(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, "return 1", got)
}

func TestMemSource_SetOverwrite(t *testing.T) {
	src := NewMemSource()
	defer func() { _ = src.Close() }()

	src.Set("k", "v1")
	src.Set("k", "v2")
	got, err := src.Load(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, "v2", got)
}

func TestMemSource_ReloadCheck_NoChange(t *testing.T) {
	src := NewMemSource()
	defer func() { _ = src.Close() }()

	src.Set("k", "v1")
	_, err := src.Load(context.Background(), "k")
	require.NoError(t, err)

	changed, err := src.ReloadCheck(context.Background(), "k")
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestMemSource_ReloadCheck_Changed(t *testing.T) {
	src := NewMemSource()
	defer func() { _ = src.Close() }()

	src.Set("k", "v1")
	_, err := src.Load(context.Background(), "k")
	require.NoError(t, err)

	src.Set("k", "v2") // bump version

	changed, err := src.ReloadCheck(context.Background(), "k")
	require.NoError(t, err)
	assert.True(t, changed)
}

func TestMemSource_ReloadCheck_NotFound(t *testing.T) {
	src := NewMemSource()
	defer func() { _ = src.Close() }()

	_, err := src.ReloadCheck(context.Background(), "missing")
	require.Error(t, err)
}

func TestMemSource_Delete(t *testing.T) {
	src := NewMemSource()
	defer func() { _ = src.Close() }()

	src.Set("k", "v")
	src.Delete("k")
	_, err := src.Load(context.Background(), "k")
	require.Error(t, err)
}

func TestMemSource_Close_NoOp(t *testing.T) {
	src := NewMemSource()
	assert.NoError(t, src.Close())
}

////////////////////////////////////////////////////////////////////////////////
// MultiSource
////////////////////////////////////////////////////////////////////////////////

// fakeSource is a minimal Source for testing MultiSource aggregation.
type fakeSource struct {
	code        string
	err         error
	delay       time.Duration // optional latency injected into Load
	closed      bool
	loadCalls   int
	reloadCalls int
	mu          sync.Mutex
}

func (f *fakeSource) Load(ctx context.Context, _ string) (string, error) {
	f.mu.Lock()
	f.loadCalls++
	f.mu.Unlock()
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if f.err != nil {
		return "", f.err
	}
	return f.code, nil
}

func (f *fakeSource) ReloadCheck(_ context.Context, _ string) (bool, error) {
	f.mu.Lock()
	f.reloadCalls++
	f.mu.Unlock()
	// Reports changed=true iff a code is configured without error.
	if f.err != nil {
		return false, f.err
	}
	return f.code != "", nil
}

func (f *fakeSource) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func TestNewMultiSource_NoSources(t *testing.T) {
	_, err := NewMultiSource(MultiStrategyFallback)
	require.Error(t, err)
}

func TestNewMultiSource_NilSource(t *testing.T) {
	_, err := NewMultiSource(MultiStrategyFallback, NewMemSource(), nil)
	require.Error(t, err)
}

func TestMultiSource_Fallback_FirstOK(t *testing.T) {
	a := &fakeSource{code: "from-a"}
	b := &fakeSource{code: "from-b"}

	ms, err := NewFallbackSource(a, b)
	require.NoError(t, err)
	defer func() { _ = ms.Close() }()

	got, err := ms.Load(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, "from-a", got)

	// Subsequent sources must be skipped on fallback.
	a.mu.Lock()
	b.mu.Lock()
	assert.Equal(t, 1, a.loadCalls)
	assert.Equal(t, 0, b.loadCalls)
	a.mu.Unlock()
	b.mu.Unlock()
}

func TestMultiSource_Fallback_SecondOK(t *testing.T) {
	a := &fakeSource{err: errors.New("boom")}
	b := &fakeSource{code: "from-b"}

	ms, err := NewFallbackSource(a, b)
	require.NoError(t, err)
	defer func() { _ = ms.Close() }()

	got, err := ms.Load(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, "from-b", got)
}

func TestMultiSource_Fallback_AllFail(t *testing.T) {
	e1 := errors.New("e1")
	e2 := errors.New("e2")
	a := &fakeSource{err: e1}
	b := &fakeSource{err: e2}

	ms, err := NewFallbackSource(a, b)
	require.NoError(t, err)
	defer func() { _ = ms.Close() }()

	_, err = ms.Load(context.Background(), "k")
	require.Error(t, err)
	// Aggregated error should mention both underlying errors.
	assert.ErrorIs(t, err, e1)
	assert.ErrorIs(t, err, e2)
}

func TestMultiSource_FirstOK_FirstReturns(t *testing.T) {
	// Make `a` return immediately and `b` return after a small delay, so the
	// first-ok strategy deterministically picks `a`.
	a := &fakeSource{code: "fast"}
	b := &fakeSource{code: "slow", delay: 50 * time.Millisecond}

	ms, err := NewFirstOKSource(a, b)
	require.NoError(t, err)
	defer func() { _ = ms.Close() }()

	got, err := ms.Load(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, "fast", got)
}

func TestMultiSource_FirstOK_AllFail(t *testing.T) {
	a := &fakeSource{err: errors.New("e1")}
	b := &fakeSource{err: errors.New("e2")}

	ms, err := NewFirstOKSource(a, b)
	require.NoError(t, err)
	defer func() { _ = ms.Close() }()

	_, err = ms.Load(context.Background(), "k")
	require.Error(t, err)
}

func TestMultiSource_ReloadCheck_AnyChanged(t *testing.T) {
	// First source has no code (ReloadCheck returns changed=false), second has
	// code (changed=true). The aggregate must report true.
	a := &fakeSource{}
	b := &fakeSource{code: "x"}

	ms, err := NewFallbackSource(a, b)
	require.NoError(t, err)
	defer func() { _ = ms.Close() }()

	changed, err := ms.ReloadCheck(context.Background(), "k")
	require.NoError(t, err)
	assert.True(t, changed)
}

func TestMultiSource_ReloadCheck_NoneChanged_WithErrors(t *testing.T) {
	// No source reports a change, but one errored: the aggregate must surface
	// the first error.
	a := &fakeSource{err: errors.New("boom")}
	b := &fakeSource{err: errors.New("boom2")}

	ms, err := NewFallbackSource(a, b)
	require.NoError(t, err)
	defer func() { _ = ms.Close() }()

	changed, err := ms.ReloadCheck(context.Background(), "k")
	assert.False(t, changed)
	require.Error(t, err)
}

func TestMultiSource_Close_ClosesAll(t *testing.T) {
	a := &fakeSource{}
	b := &fakeSource{}

	ms, err := NewFallbackSource(a, b)
	require.NoError(t, err)

	require.NoError(t, ms.Close())
	assert.True(t, a.closed)
	assert.True(t, b.closed)
}

func TestMultiSource_Load_CancelledContext(t *testing.T) {
	a := &fakeSource{code: "x"}
	ms, err := NewFallbackSource(a)
	require.NoError(t, err)
	defer func() { _ = ms.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = ms.Load(ctx, "k")
	require.ErrorIs(t, err, context.Canceled)
}
