// Package etcd provides a [source.Reader] implementation that reads
// scripts from an etcd cluster. It leverages etcd's native Watch API for
// efficient hot-reload detection without polling.
//
// Construction:
//
//	src, err := etcd.New(ctx,
//	    etcd.WithEndpoints("localhost:2379"),
//	    etcd.WithPrefix("scripts/lua/"),
//	)
//
// Hot-reload uses etcd's built-in Watch mechanism: a watch is established on
// the key and a signal is sent whenever a PUT or DELETE event occurs.
package etcd

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/tx7do/go-scripts/source"
)

// Reader reads scripts from etcd.
//
// All exported methods are safe for concurrent use. Reader implements the
// [source.ReadWatcher] interface.
type Reader struct {
	client clientAPI
	closer ioCloser

	prefix string

	mu     sync.RWMutex
	values map[string]string // key -> last loaded value
}

// Compile-time assertion: *Reader implements source.Reader.
var _ source.Reader = (*Reader)(nil)

// Compile-time assertion: *Reader also implements source.ReadWatcher.
var _ source.ReadWatcher = (*Reader)(nil)

// withClient is an internal option used by tests to inject a fake [clientAPI].
// Not exported.
func withClient(c clientAPI, closer ioCloser) Option {
	return func(o *configOptions) {
		o.client = c
		o.closer = closer
	}
}

// New creates an etcd-backed [Reader]. At least one endpoint must be supplied
// via WithEndpoints (default is "localhost:2379"). All other settings are
// optional.
//
// Authentication can be enabled via WithUsername / WithPassword.
func New(_ context.Context, opts ...Option) (*Reader, error) {
	cfg := &configOptions{
		endpoints: []string{"localhost:2379"},
		timeout:   5 * time.Second,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Test-injected client bypasses the real etcd SDK entirely.
	if cfg.client != nil {
		return &Reader{
			client: cfg.client,
			closer: cfg.closer,
			prefix: cfg.prefix,
			values: make(map[string]string),
		}, nil
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   cfg.endpoints,
		DialTimeout: cfg.timeout,
		Username:    cfg.username,
		Password:    cfg.password,
	})
	if err != nil {
		return nil, fmt.Errorf("etcd source: create client: %w", err)
	}

	return &Reader{
		client: cli,
		closer: cli,
		prefix: cfg.prefix,
		values: make(map[string]string),
	}, nil
}

// resolveKey prepends the configured prefix to the user-supplied key.
func (r *Reader) resolveKey(key string) string {
	return r.prefix + key
}

// Load fetches the value from etcd and returns it as a string.
// Context cancellation propagates to the underlying request.
//
// An empty response (key not found) is reported as a wrapped [ErrNotFound].
// Other errors are wrapped with the key for easier debugging.
func (r *Reader) Load(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	etcdKey := r.resolveKey(key)
	resp, err := r.client.Get(ctx, etcdKey)
	if err != nil {
		return "", fmt.Errorf("etcd source: get %q: %w", etcdKey, err)
	}

	if len(resp.Kvs) == 0 {
		return "", fmt.Errorf("%w: %s", ErrNotFound, etcdKey)
	}

	val := string(resp.Kvs[0].Value)

	r.mu.Lock()
	r.values[key] = val
	r.mu.Unlock()

	return val, nil
}

// Close releases the underlying etcd client.
func (r *Reader) Close() error {
	if r.closer != nil {
		return r.closer.Close()
	}
	return nil
}

// IsNotFound reports whether err represents a "key not found" response from
// etcd. Equivalent to errors.Is(err, ErrNotFound).
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
