package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestFileSource_Close_NoOp(t *testing.T) {
	src := NewFileSource()
	assert.NoError(t, src.Close())
	// Idempotent.
	assert.NoError(t, src.Close())
}

func TestFileSource_Watch_FileModified(t *testing.T) {
	path := writeTempFile(t, "a.lua", "v1")
	src := NewFileSource()
	defer func() { _ = src.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch, err := src.Watch(ctx, path)
	require.NoError(t, err)

	// Modify the file after a short delay.
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, os.WriteFile(path, []byte("v2"), 0o644))

	// Wait for the watch signal (polling interval is 1s).
	select {
	case <-ch:
		// Signal received as expected.
	case <-ctx.Done():
		t.Fatal("timeout waiting for watch signal")
	}
}

func TestFileSource_Watch_ContextCancelled(t *testing.T) {
	path := writeTempFile(t, "a.lua", "v1")
	src := NewFileSource()
	defer func() { _ = src.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := src.Watch(ctx, path)
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

func TestFileSource_Watch_MissingFile(t *testing.T) {
	src := NewFileSource()
	defer func() { _ = src.Close() }()

	_, err := src.Watch(context.Background(), filepath.Join(t.TempDir(), "missing.lua"))
	require.Error(t, err)
}
