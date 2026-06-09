package source

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSource is a minimal Reader for testing MultiSource aggregation.
type fakeSource struct {
	code      string
	err       error
	delay     time.Duration // optional latency injected into Load
	closed    bool
	loadCalls int
	mu        sync.Mutex
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

// fakeWatchableSource is a Reader + Watcher for testing MultiSource.Watch.
type fakeWatchableSource struct {
	fakeSource
	watchCalled bool
	watchCh     chan struct{}
}

func (f *fakeWatchableSource) Watch(ctx context.Context, key string) (<-chan struct{}, error) {
	f.mu.Lock()
	f.watchCalled = true
	f.mu.Unlock()
	return f.watchCh, nil
}

func TestMultiSource_Watch_DelegatesToFirstWatcher(t *testing.T) {
	// First source supports Watch, second doesn't.
	mem := NewMemSource()
	mem.Set("k", "v1")
	fake := &fakeSource{code: "from-fake"}

	ms, err := NewFallbackSource(mem, fake)
	require.NoError(t, err)
	defer func() { _ = ms.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := ms.Watch(ctx, "k")
	require.NoError(t, err)

	// Trigger a change in MemSource.
	time.Sleep(50 * time.Millisecond)
	mem.Set("k", "v2")

	select {
	case <-ch:
		// Signal received as expected.
	case <-ctx.Done():
		t.Fatal("timeout waiting for watch signal")
	}
}

func TestMultiSource_Watch_NoWatcherAvailable(t *testing.T) {
	// Both sources only implement Reader, not Watcher.
	a := &fakeSource{code: "a"}
	b := &fakeSource{code: "b"}

	ms, err := NewFallbackSource(a, b)
	require.NoError(t, err)
	defer func() { _ = ms.Close() }()

	_, err = ms.Watch(context.Background(), "k")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "none of the sub-sources implement Watcher")
}
