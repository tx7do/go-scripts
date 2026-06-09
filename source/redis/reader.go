// Package redis provides a [source.Reader] implementation that reads
// scripts from a Redis key-value store (or any Redis-compatible server such
// as Valkey, DragonflyDB, KeyDB, ...).
//
// Construction:
//
//	src, err := redis.New(ctx,
//	    redis.WithAddr("localhost:6379"),
//	    redis.WithPrefix("scripts:lua:"),
//	)
//
// Hot-reload detection uses client-side polling: the value returned by the
// most recent Load is kept in memory, and Watch polls compare the current
// value against the stored one.
package redis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/redis/go-redis/v9"

	"github.com/tx7do/go-scripts/source"
)

// Reader reads scripts from Redis.
//
// All exported methods are safe for concurrent use. Reader implements the
// [source.ReadWatcher] interface.
type Reader struct {
	client redisCmdable
	closer io.Closer

	prefix string

	mu     sync.RWMutex
	values map[string]string // key -> last loaded value (for change detection)
}

// Compile-time assertion: *Reader implements source.Reader.
var _ source.Reader = (*Reader)(nil)

// Compile-time assertion: *Reader also implements source.ReadWatcher.
var _ source.ReadWatcher = (*Reader)(nil)

// withClient is an internal option used by tests to inject a fake [redisCmdable].
// Not exported.
func withClient(c redisCmdable, closer io.Closer) Option {
	return func(o *configOptions) {
		o.client = c
		o.closer = closer
	}
}

// New creates a Redis-backed [Reader]. All settings are optional; defaults are
// "localhost:6379", DB 0, no password.
//
// Credentials can be supplied via WithPassword / WithUsername. For Redis ACL
// users, set both username and password.
func New(_ context.Context, opts ...Option) (*Reader, error) {
	cfg := &configOptions{
		addr: "localhost:6379",
		db:   0,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Test-injected client bypasses the real Redis SDK entirely.
	if cfg.client != nil {
		return &Reader{
			client: cfg.client,
			closer: cfg.closer,
			prefix: cfg.prefix,
			values: make(map[string]string),
		}, nil
	}

	client := redis.NewClient(&redis.Options{
		Addr:     cfg.addr,
		Password: cfg.password,
		Username: cfg.username,
		DB:       cfg.db,
	})

	return &Reader{
		client: client,
		closer: client,
		prefix: cfg.prefix,
		values: make(map[string]string),
	}, nil
}

// resolveKey prepends the configured prefix to the user-supplied key.
func (r *Reader) resolveKey(key string) string {
	return r.prefix + key
}

// Load fetches the value from Redis and returns it as a string.
// Context cancellation propagates to the underlying request.
//
// A redis.Nil response is reported as a wrapped [ErrNotFound].
// Other errors are wrapped with the key for easier debugging.
func (r *Reader) Load(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	redisKey := r.resolveKey(key)
	val, err := r.client.Get(ctx, redisKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", fmt.Errorf("%w: %s", ErrNotFound, redisKey)
		}
		return "", fmt.Errorf("redis source: get %q: %w", redisKey, err)
	}

	r.mu.Lock()
	r.values[key] = val
	r.mu.Unlock()

	return val, nil
}

// Close releases the underlying Redis client.
func (r *Reader) Close() error {
	if r.closer != nil {
		return r.closer.Close()
	}
	return nil
}

// IsNotFound reports whether err represents a "key not found" response from
// Redis. Equivalent to errors.Is(err, ErrNotFound).
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// hasValueChanged compares the current value in Redis against the value stored
// from the last Load. Returns true if the value differs or the key was deleted.
func (r *Reader) hasValueChanged(ctx context.Context, key string) bool {
	redisKey := r.resolveKey(key)
	val, err := r.client.Get(ctx, redisKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// Key was deleted since last Load.
			r.mu.RLock()
			_, existed := r.values[key]
			r.mu.RUnlock()
			return existed
		}
		return false // can't determine; don't signal.
	}

	r.mu.RLock()
	stored := r.values[key]
	r.mu.RUnlock()
	return val != stored
}
