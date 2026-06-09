package source

import (
	"context"
	"fmt"
)

// TransformFunc is a hook that transforms the raw script source after it is
// loaded from the underlying Reader but before it is returned to the caller.
//
// Typical use cases:
//   - Decryption: decrypt AES/XXTEA encrypted scripts stored in S3, DB, etc.
//   - Decompression: decompress gzip/zstd compressed scripts.
//   - Validation / sanitization: strip sensitive information.
//   - Encoding conversion: convert from legacy encoding to UTF-8.
//
// The `key` parameter is the original key passed to Load, useful for
// determining which transform to apply per-key (e.g., some keys may be
// encrypted, others not).
//
// If the transform fails, the error propagates to the caller of Load without
// any fallback.
type TransformFunc func(key string, raw string) (string, error)

// TransformSource wraps a [Reader] and applies one or more [TransformFunc]
// hooks to the loaded source code. Transforms are applied in registration
// order: the output of transform N becomes the input of transform N+1.
//
// If the wrapped Reader also implements [Watcher], TransformSource delegates
// Watch calls transparently — the transform is only applied to Load, not to
// Watch signals. Callers should re-Load after receiving a Watch signal to
// get the freshly transformed content.
//
// All exported methods are safe for concurrent use. TransformSource implements
// the [ReadWatcher] interface.
type TransformSource struct {
	inner      Reader
	transforms []TransformFunc
}

// Compile-time assertion: *TransformSource implements source.Reader.
var _ Reader = (*TransformSource)(nil)

// Compile-time assertion: *TransformSource implements source.ReadWatcher.
var _ ReadWatcher = (*TransformSource)(nil)

// NewTransformSource wraps the given Reader with one or more transform
// functions. At least one transform must be provided.
//
// Transforms are applied in order: if you pass [decrypt, decompress], the
// raw data is first decrypted, then decompressed.
func NewTransformSource(inner Reader, transforms ...TransformFunc) (*TransformSource, error) {
	if inner == nil {
		return nil, fmt.Errorf("transform source: inner reader is nil")
	}
	if len(transforms) == 0 {
		return nil, fmt.Errorf("transform source: at least one transform function is required")
	}
	for i, t := range transforms {
		if t == nil {
			return nil, fmt.Errorf("transform source: transform[%d] is nil", i)
		}
	}
	return &TransformSource{inner: inner, transforms: transforms}, nil
}

// Load fetches the script from the underlying Reader, then applies all
// registered transforms in sequence.
func (ts *TransformSource) Load(ctx context.Context, key string) (string, error) {
	raw, err := ts.inner.Load(ctx, key)
	if err != nil {
		return "", fmt.Errorf("transform source: load %q: %w", key, err)
	}

	result := raw
	for i, fn := range ts.transforms {
		result, err = fn(key, result)
		if err != nil {
			return "", fmt.Errorf("transform source: transform[%d] for %q: %w", i, key, err)
		}
	}
	return result, nil
}

// Close releases the underlying Reader.
func (ts *TransformSource) Close() error {
	return ts.inner.Close()
}

// Watch delegates to the underlying Reader if it implements [Watcher].
// Returns an error if the inner source does not support watching.
func (ts *TransformSource) Watch(ctx context.Context, key string) (<-chan struct{}, error) {
	w, ok := ts.inner.(Watcher)
	if !ok {
		return nil, fmt.Errorf("transform source: inner reader %T does not implement Watcher", ts.inner)
	}
	return w.Watch(ctx, key)
}

// Then chains an additional transform to an existing TransformSource.
// Returns a new TransformSource with the additional transform appended.
func (ts *TransformSource) Then(fn TransformFunc) (*TransformSource, error) {
	if fn == nil {
		return nil, fmt.Errorf("transform source: then: transform function is nil")
	}
	return &TransformSource{
		inner:      ts.inner,
		transforms: append(append([]TransformFunc{}, ts.transforms...), fn),
	}, nil
}

// IdentityTransform is a no-op transform that returns the input unchanged.
// Useful as a placeholder or for testing.
func IdentityTransform(_ string, raw string) (string, error) {
	return raw, nil
}
