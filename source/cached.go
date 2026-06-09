package source

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CachedSource wraps a remote [Reader] (e.g. S3, database, HTTP) with an
// in-memory cache backed by [MemSource]. On Load, it first checks the cache;
// only on a cache miss (or after a Watch signal invalidates the entry) does it
// fetch from the remote source.
//
// This dramatically reduces network round-trips for hot-path script loading,
// which is critical for game servers and microservices that execute scripts
// thousands of times per second.
//
// If the remote source implements [Watcher], CachedSource automatically starts
// a background goroutine per key to listen for invalidation signals. When a
// change is detected, the cached entry is evicted so the next Load fetches the
// fresh content from the remote.
//
// All exported methods are safe for concurrent use. CachedSource implements
// the [ReadWatcher] interface.
type CachedSource struct {
	remote Reader
	cache  *MemSource

	mu       sync.RWMutex
	loaded   map[string]struct{}           // keys that have been loaded at least once
	watchers map[string]context.CancelFunc // active watcher cancellations

	// ttl is the maximum time a cached entry is considered fresh.
	// Zero means no TTL (entries only invalidated by Watch).
	ttl time.Duration

	// timestamps tracks when each key was last fetched from remote.
	// Used for TTL expiry.
	tsMu sync.Mutex
	ts   map[string]time.Time
}

// Compile-time assertion: *CachedSource implements source.Reader.
var _ Reader = (*CachedSource)(nil)

// Compile-time assertion: *CachedSource implements source.ReadWatcher.
var _ ReadWatcher = (*CachedSource)(nil)

// CachedOption configures a [CachedSource].
type CachedOption func(*CachedSource)

// WithTTL sets the time-to-live for cached entries. After TTL expires, the
// next Load fetches from the remote source regardless of Watch status.
// A zero duration (default) disables TTL-based expiry.
func WithTTL(ttl time.Duration) CachedOption {
	return func(c *CachedSource) { c.ttl = ttl }
}

// NewCachedSource wraps the given remote Reader with an in-memory cache.
// The remote reader must not be nil.
//
// If the remote implements [Watcher], cached entries are automatically
// invalidated when the remote signals a change. Otherwise, entries persist
// until TTL expiry (if set) or manual Invalidate.
func NewCachedSource(remote Reader, opts ...CachedOption) (*CachedSource, error) {
	if remote == nil {
		return nil, fmt.Errorf("cached source: remote reader is nil")
	}
	c := &CachedSource{
		remote:   remote,
		cache:    NewMemSource(),
		loaded:   make(map[string]struct{}),
		watchers: make(map[string]context.CancelFunc),
		ts:       make(map[string]time.Time),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Load returns the cached script if available and fresh; otherwise fetches
// from the remote source and updates the cache.
func (c *CachedSource) Load(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Fast path: check cache.
	if c.isFresh(key) {
		if code, err := c.cache.Load(ctx, key); err == nil {
			return code, nil
		}
	}

	// Slow path: fetch from remote.
	code, err := c.remote.Load(ctx, key)
	if err != nil {
		return "", fmt.Errorf("cached source: remote load %q: %w", key, err)
	}

	// Store in cache.
	c.cache.Set(key, code)
	c.tsMu.Lock()
	c.ts[key] = time.Now()
	c.tsMu.Unlock()

	c.mu.Lock()
	if _, exists := c.loaded[key]; !exists {
		c.loaded[key] = struct{}{}
		// Start background watcher if the remote supports it.
		c.startWatcherLocked(key)
	}
	c.mu.Unlock()

	return code, nil
}

// Close releases the remote reader and stops all background watchers.
func (c *CachedSource) Close() error {
	c.mu.Lock()
	for key, cancel := range c.watchers {
		cancel()
		delete(c.watchers, key)
	}
	c.mu.Unlock()
	return c.remote.Close()
}

// Invalidate removes a key from the cache, forcing the next Load to fetch
// from the remote.
func (c *CachedSource) Invalidate(key string) {
	c.cache.Delete(key)
	c.tsMu.Lock()
	delete(c.ts, key)
	c.tsMu.Unlock()
}

// InvalidateAll clears the entire cache.
func (c *CachedSource) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key := range c.loaded {
		c.cache.Delete(key)
	}
	c.tsMu.Lock()
	c.ts = make(map[string]time.Time)
	c.tsMu.Unlock()
}

// isFresh checks whether the cached entry for the given key exists and has
// not expired (if TTL is set).
func (c *CachedSource) isFresh(key string) bool {
	c.mu.RLock()
	_, loaded := c.loaded[key]
	c.mu.RUnlock()
	if !loaded {
		return false
	}

	if c.ttl > 0 {
		c.tsMu.Lock()
		ts, ok := c.ts[key]
		c.tsMu.Unlock()
		if !ok || time.Since(ts) > c.ttl {
			return false
		}
	}

	return true
}

// startWatcherLocked starts a background goroutine that listens for Watch
// signals from the remote source and invalidates the cache on change.
// Caller must hold c.mu write lock.
func (c *CachedSource) startWatcherLocked(key string) {
	w, ok := c.remote.(Watcher)
	if !ok {
		return // remote does not support watching; TTL-only invalidation.
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.watchers[key] = cancel

	go func() {
		ch, err := w.Watch(ctx, key)
		if err != nil {
			// Watch failed; rely on TTL or manual invalidation.
			return
		}
		for range ch {
			c.cache.Delete(key)
			c.tsMu.Lock()
			delete(c.ts, key)
			c.tsMu.Unlock()
		}
	}()
}

// Watch delegates to the remote source if it implements [Watcher].
// Returns an error if the remote source does not support watching.
func (c *CachedSource) Watch(ctx context.Context, key string) (<-chan struct{}, error) {
	w, ok := c.remote.(Watcher)
	if !ok {
		return nil, fmt.Errorf("cached source: remote %T does not implement Watcher", c.remote)
	}
	return w.Watch(ctx, key)
}
