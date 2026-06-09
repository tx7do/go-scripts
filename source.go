package script_engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// Source represents a script source.
// key is the unique identifier of a script: path / object key / script id, etc.,
// interpreted by the concrete implementation.
type Source interface {
	// Load loads the script source code.
	Load(ctx context.Context, key string) (code string, err error)

	// ReloadCheck reports whether the source has changed since the last Load (for hot reload).
	// A changed=true result means the caller should Load again.
	ReloadCheck(ctx context.Context, key string) (changed bool, err error)

	// Close releases underlying resources (s3 client, file handles, etc.).
	Close() error
}

////////////////////////////////////////////////////////////////////////////////
// FileSource
////////////////////////////////////////////////////////////////////////////////

// FileSource reads scripts from the local filesystem.
// It has no extra dependencies and is the default choice for dev/debug.
// Hot reload is detected by comparing file mtime.
type FileSource struct {
	mu     sync.RWMutex
	mtimes map[string]time.Time // key -> file mtime recorded at the last Load
}

// NewFileSource creates a FileSource.
func NewFileSource() *FileSource {
	return &FileSource{mtimes: make(map[string]time.Time)}
}

// Load reads the local file at the given key (path).
func (fs *FileSource) Load(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	b, err := os.ReadFile(key)
	if err != nil {
		return "", fmt.Errorf("file source: read %q: %w", key, err)
	}
	if fi, statErr := os.Stat(key); statErr == nil {
		fs.mu.Lock()
		fs.mtimes[key] = fi.ModTime()
		fs.mu.Unlock()
	}
	return string(b), nil
}

// ReloadCheck compares the file mtime against the last recorded value.
// A key that has never been loaded is treated as changed=true.
func (fs *FileSource) ReloadCheck(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	fi, err := os.Stat(key)
	if err != nil {
		return false, fmt.Errorf("file source: stat %q: %w", key, err)
	}
	fs.mu.RLock()
	old, ok := fs.mtimes[key]
	fs.mu.RUnlock()
	if !ok {
		return true, nil
	}
	return !fi.ModTime().Equal(old), nil
}

// Close is a no-op: FileSource holds no resources that need releasing.
func (fs *FileSource) Close() error { return nil }

////////////////////////////////////////////////////////////////////////////////
// MemSource
////////////////////////////////////////////////////////////////////////////////

type memEntry struct {
	code       string
	currentVer int64 // incremented on every Set
	loadedVer  int64 // snapshot of currentVer seen by the last Load
}

// MemSource keeps scripts in memory.
// Suitable for dynamic short-lived scripts, unit tests, or RPC-pushed snippets;
// zero IO overhead.
type MemSource struct {
	mu   sync.Mutex
	data map[string]*memEntry
}

// NewMemSource creates a MemSource.
func NewMemSource() *MemSource {
	return &MemSource{data: make(map[string]*memEntry)}
}

// Set inserts or overwrites a script. The internal version is incremented so
// the next ReloadCheck reports changed=true.
func (ms *MemSource) Set(key, code string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	e, ok := ms.data[key]
	if !ok {
		e = &memEntry{}
		ms.data[key] = e
	}
	e.code = code
	e.currentVer++
}

// Delete removes a script.
func (ms *MemSource) Delete(key string) {
	ms.mu.Lock()
	delete(ms.data, key)
	ms.mu.Unlock()
}

// Load returns the script for the given key and syncs loadedVer to currentVer.
func (ms *MemSource) Load(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()
	e, ok := ms.data[key]
	if !ok {
		return "", fmt.Errorf("mem source: key %q not found", key)
	}
	e.loadedVer = e.currentVer
	return e.code, nil
}

// ReloadCheck reports changed=true when currentVer differs from loadedVer,
// i.e. a newer version has been Set since the last Load.
// Returns an error if the key does not exist.
func (ms *MemSource) ReloadCheck(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()
	e, ok := ms.data[key]
	if !ok {
		return false, fmt.Errorf("mem source: key %q not found", key)
	}
	return e.currentVer != e.loadedVer, nil
}

// Close is a no-op: MemSource holds no resources that need releasing.
func (ms *MemSource) Close() error { return nil }

////////////////////////////////////////////////////////////////////////////////
// MultiSource
////////////////////////////////////////////////////////////////////////////////

// MultiStrategy selects how MultiSource aggregates its sub-sources.
type MultiStrategy int

const (
	// MultiStrategyFallback tries sub-sources in order; the first success wins and
	// subsequent sources are skipped. Suitable for "S3 primary + local backup" scenarios.
	MultiStrategyFallback MultiStrategy = iota

	// MultiStrategyFirstOK fetches from all sub-sources concurrently and returns the
	// first successful result. Suitable for low-latency reads across mirrored sources.
	MultiStrategyFirstOK
)

// MultiSource aggregates multiple sub-sources under a single Source interface.
// The strategy controls how Load selects among them.
type MultiSource struct {
	sources  []Source
	strategy MultiStrategy
}

// NewMultiSource creates a MultiSource. At least one sub-source is required.
func NewMultiSource(strategy MultiStrategy, sources ...Source) (*MultiSource, error) {
	if len(sources) == 0 {
		return nil, errors.New("multi source: at least one source is required")
	}
	for i, s := range sources {
		if s == nil {
			return nil, fmt.Errorf("multi source: source[%d] is nil", i)
		}
	}
	return &MultiSource{sources: sources, strategy: strategy}, nil
}

// NewFallbackSource is a shortcut for NewMultiSource(MultiStrategyFallback, sources...).
func NewFallbackSource(sources ...Source) (*MultiSource, error) {
	return NewMultiSource(MultiStrategyFallback, sources...)
}

// NewFirstOKSource is a shortcut for NewMultiSource(MultiStrategyFirstOK, sources...).
func NewFirstOKSource(sources ...Source) (*MultiSource, error) {
	return NewMultiSource(MultiStrategyFirstOK, sources...)
}

// Load dispatches to the strategy-specific loader.
func (ms *MultiSource) Load(ctx context.Context, key string) (string, error) {
	switch ms.strategy {
	case MultiStrategyFirstOK:
		return ms.loadFirstOK(ctx, key)
	default: // MultiStrategyFallback
		return ms.loadFallback(ctx, key)
	}
}

func (ms *MultiSource) loadFallback(ctx context.Context, key string) (string, error) {
	var errs []error
	for _, s := range ms.sources {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		code, err := s.Load(ctx, key)
		if err == nil {
			return code, nil
		}
		errs = append(errs, err)
	}
	return "", fmt.Errorf("multi source(fallback): all sources failed for %q: %w", key, errors.Join(errs...))
}

func (ms *MultiSource) loadFirstOK(ctx context.Context, key string) (string, error) {
	type result struct {
		code string
		err  error
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch := make(chan result, len(ms.sources))
	for _, s := range ms.sources {
		s := s
		go func() {
			code, err := s.Load(ctx, key)
			ch <- result{code, err}
		}()
	}

	var errs []error
	for i := 0; i < len(ms.sources); i++ {
		r := <-ch
		if r.err == nil {
			cancel() // tell other goroutines to bail out ASAP
			return r.code, nil
		}
		errs = append(errs, r.err)
	}
	return "", fmt.Errorf("multi source(first-ok): all sources failed for %q: %w", key, errors.Join(errs...))
}

// ReloadCheck returns true if any sub-source reports a change for the key.
// A change in any single sub-source is treated as needing a reload.
func (ms *MultiSource) ReloadCheck(ctx context.Context, key string) (bool, error) {
	var firstErr error
	for _, s := range ms.sources {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		changed, err := s.ReloadCheck(ctx, key)
		if err == nil {
			if changed {
				return true, nil
			}
			continue
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	// No sub-source reported a change. If any sub-source errored (without reporting
	// a change), surface the first such error.
	return false, firstErr
}

// Close closes every sub-source and returns the aggregated error.
func (ms *MultiSource) Close() error {
	var errs []error
	for _, s := range ms.sources {
		if err := s.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("multi source: close errors: %w", errors.Join(errs...))
}
