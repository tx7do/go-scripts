package source

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestMemSource_Watch_SetTriggers(t *testing.T) {
	src := NewMemSource()
	defer func() { _ = src.Close() }()

	src.Set("k", "v1")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := src.Watch(ctx, "k")
	require.NoError(t, err)

	// Trigger a change.
	time.Sleep(50 * time.Millisecond)
	src.Set("k", "v2")

	select {
	case <-ch:
		// Signal received as expected.
	case <-ctx.Done():
		t.Fatal("timeout waiting for watch signal")
	}
}

func TestMemSource_Watch_DeleteTriggers(t *testing.T) {
	src := NewMemSource()
	defer func() { _ = src.Close() }()

	src.Set("k", "v1")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := src.Watch(ctx, "k")
	require.NoError(t, err)

	// Delete the key.
	time.Sleep(50 * time.Millisecond)
	src.Delete("k")

	select {
	case <-ch:
		// Signal received as expected.
	case <-ctx.Done():
		t.Fatal("timeout waiting for watch signal")
	}
}

func TestMemSource_Watch_ContextCancelled(t *testing.T) {
	src := NewMemSource()
	defer func() { _ = src.Close() }()

	src.Set("k", "v1")

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := src.Watch(ctx, "k")
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
