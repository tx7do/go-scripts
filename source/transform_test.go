package source

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTransformSource_ImplementsInterface is a compile-time guard.
func TestTransformSource_ImplementsInterface(t *testing.T) {
	var _ Reader = (*TransformSource)(nil)
	var _ ReadWatcher = (*TransformSource)(nil)
}

// TestNewTransformSource_NilInner verifies that a nil inner reader is rejected.
func TestNewTransformSource_NilInner(t *testing.T) {
	_, err := NewTransformSource(nil, IdentityTransform)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inner reader is nil")
}

// TestNewTransformSource_NoTransforms verifies that at least one transform is required.
func TestNewTransformSource_NoTransforms(t *testing.T) {
	mem := NewMemSource()
	_, err := NewTransformSource(mem)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one transform")
}

// TestNewTransformSource_NilTransform verifies that nil transforms are rejected.
func TestNewTransformSource_NilTransform(t *testing.T) {
	mem := NewMemSource()
	_, err := NewTransformSource(mem, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

// TestLoad_SingleTransform verifies that a single transform is applied.
func TestLoad_SingleTransform(t *testing.T) {
	mem := NewMemSource()
	mem.Set("script.lua", "hello world")

	// Uppercase transform.
	toUpper := TransformFunc(func(_ string, raw string) (string, error) {
		return strings.ToUpper(raw), nil
	})

	src, err := NewTransformSource(mem, toUpper)
	require.NoError(t, err)
	defer src.Close()

	code, err := src.Load(context.Background(), "script.lua")
	require.NoError(t, err)
	assert.Equal(t, "HELLO WORLD", code)
}

// TestLoad_ChainedTransforms verifies that multiple transforms are applied in order.
func TestLoad_ChainedTransforms(t *testing.T) {
	mem := NewMemSource()
	mem.Set("script.lua", "  hello  ")

	trim := TransformFunc(func(_ string, raw string) (string, error) {
		return strings.TrimSpace(raw), nil
	})
	toUpper := TransformFunc(func(_ string, raw string) (string, error) {
		return strings.ToUpper(raw), nil
	})

	src, err := NewTransformSource(mem, trim, toUpper)
	require.NoError(t, err)
	defer src.Close()

	code, err := src.Load(context.Background(), "script.lua")
	require.NoError(t, err)
	assert.Equal(t, "HELLO", code)
}

// TestLoad_TransformError verifies that a transform error propagates.
func TestLoad_TransformError(t *testing.T) {
	mem := NewMemSource()
	mem.Set("script.lua", "data")

	customErr := errors.New("decrypt failed")
	decrypt := TransformFunc(func(_ string, _ string) (string, error) {
		return "", customErr
	})

	src, err := NewTransformSource(mem, decrypt)
	require.NoError(t, err)
	defer src.Close()

	_, err = src.Load(context.Background(), "script.lua")
	require.Error(t, err)
	assert.ErrorIs(t, err, customErr)
	assert.Contains(t, err.Error(), "transform[0]")
}

// TestLoad_InnerError verifies that inner Load errors propagate.
func TestLoad_InnerError(t *testing.T) {
	mem := NewMemSource()
	// No data set; Load will fail.

	src, err := NewTransformSource(mem, IdentityTransform)
	require.NoError(t, err)
	defer src.Close()

	_, err = src.Load(context.Background(), "missing.lua")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transform source: load")
}

// TestLoad_KeySpecificTransform verifies that transforms can use the key
// to decide per-key behavior.
func TestLoad_KeySpecificTransform(t *testing.T) {
	mem := NewMemSource()
	mem.Set("encrypted.lua", "ENCRYPTED_DATA")
	mem.Set("plain.lua", "print('hello')")

	decryptIfNeeded := TransformFunc(func(key string, raw string) (string, error) {
		if strings.HasPrefix(key, "encrypted") {
			// Simulate decryption: reverse the string.
			runes := []rune(raw)
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}
			return string(runes), nil
		}
		return raw, nil
	})

	src, err := NewTransformSource(mem, decryptIfNeeded)
	require.NoError(t, err)
	defer src.Close()

	// Encrypted key gets transformed.
	code, err := src.Load(context.Background(), "encrypted.lua")
	require.NoError(t, err)
	assert.Equal(t, "ATAD_DETPYRCNE", code)

	// Plain key passes through.
	code, err = src.Load(context.Background(), "plain.lua")
	require.NoError(t, err)
	assert.Equal(t, "print('hello')", code)
}

// TestLoad_Concurrent verifies that concurrent Loads are safe.
func TestTransformSource_Load_Concurrent(t *testing.T) {
	mem := NewMemSource()
	mem.Set("shared.lua", "body")

	src, err := NewTransformSource(mem, IdentityTransform)
	require.NoError(t, err)
	defer src.Close()

	const goroutines = 30
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			code, err := src.Load(context.Background(), "shared.lua")
			assert.NoError(t, err)
			assert.Equal(t, "body", code)
		}()
	}
	wg.Wait()
}

// TestTransformSource_Watch_Delegates verifies that Watch delegates to inner.
func TestTransformSource_Watch_Delegates(t *testing.T) {
	mem := NewMemSource()
	mem.Set("script.lua", "v1")

	src, err := NewTransformSource(mem, IdentityTransform)
	require.NoError(t, err)
	defer src.Close()

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := src.Watch(ctx, "script.lua")
	require.NoError(t, err)

	// Trigger a change.
	time.Sleep(50 * time.Millisecond)
	mem.Set("script.lua", "v2")

	select {
	case <-ch:
		// Signal received.
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("timeout waiting for watch signal")
	}

	cancel()
}

// TestTransformSource_Watch_NotSupported verifies error when inner doesn't support Watch.
func TestTransformSource_Watch_NotSupported(t *testing.T) {
	// fakeSource only implements Reader, not Watcher.
	inner := &fakeSource{code: "x"}

	src, err := NewTransformSource(inner, IdentityTransform)
	require.NoError(t, err)
	defer src.Close()

	_, err = src.Watch(context.Background(), "k")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not implement Watcher")
}

// TestThen_ChainsTransform verifies that Then adds a new transform.
func TestThen_ChainsTransform(t *testing.T) {
	mem := NewMemSource()
	mem.Set("k", "hello")

	toUpper := TransformFunc(func(_ string, raw string) (string, error) {
		return strings.ToUpper(raw), nil
	})
	exclaim := TransformFunc(func(_ string, raw string) (string, error) {
		return raw + "!", nil
	})

	src, err := NewTransformSource(mem, toUpper)
	require.NoError(t, err)

	src2, err := src.Then(exclaim)
	require.NoError(t, err)
	defer src2.Close()

	code, err := src2.Load(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, "HELLO!", code)
}

// TestThen_NilTransform verifies that Then rejects nil.
func TestThen_NilTransform(t *testing.T) {
	mem := NewMemSource()
	src, err := NewTransformSource(mem, IdentityTransform)
	require.NoError(t, err)

	_, err = src.Then(nil)
	require.Error(t, err)
}

// TestIdentityTransform verifies the identity transform is a no-op.
func TestIdentityTransform(t *testing.T) {
	result, err := IdentityTransform("key", "raw data")
	require.NoError(t, err)
	assert.Equal(t, "raw data", result)
}
