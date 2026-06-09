// Package consul provides a [source.Reader] implementation that reads
// scripts from a HashiCorp Consul KV store (or any Consul-compatible agent
// such as Nomad's embedded KV, etc.).
//
// Construction:
//
//	src, err := consul.New(ctx,
//	    consul.WithAddress("127.0.0.1:8500"),
//	    consul.WithPrefix("scripts/lua/"),
//	)
//
// Hot-reload detection polls the key's ModifyIndex via Get every 5 seconds
// and sends a signal when the index changes.
package consul

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/tx7do/go-scripts/source"
)

// Reader reads scripts from Consul's KV store.
//
// All exported methods are safe for concurrent use. Reader implements the
// [source.ReadWatcher] interface.
type Reader struct {
	client consulAPI
	closer ioCloser

	prefix string

	mu     sync.RWMutex
	values map[string]string // key -> last loaded value
	indexs map[string]uint64 // key -> last ModifyIndex
}

// Compile-time assertion: *Reader implements source.Reader.
var _ source.Reader = (*Reader)(nil)

// Compile-time assertion: *Reader also implements source.ReadWatcher.
var _ source.ReadWatcher = (*Reader)(nil)

// withClient is an internal option used by tests to inject a fake [consulAPI].
// Not exported.
func withClient(c consulAPI, closer ioCloser) Option {
	return func(o *configOptions) {
		o.client = c
		o.closer = closer
	}
}

// New creates a Consul-backed [Reader]. Address defaults to "127.0.0.1:8500";
// override with WithAddress. All other settings are optional.
//
// Authentication can be enabled via WithToken.
func New(_ context.Context, opts ...Option) (*Reader, error) {
	cfg := &configOptions{
		address: "127.0.0.1:8500",
		timeout: 5 * time.Second,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Test-injected client bypasses the real Consul SDK entirely.
	if cfg.client != nil {
		return &Reader{
			client: cfg.client,
			closer: cfg.closer,
			prefix: cfg.prefix,
			values: make(map[string]string),
			indexs: make(map[string]uint64),
		}, nil
	}

	consulCfg := api.DefaultConfig()
	if cfg.address != "" {
		consulCfg.Address = cfg.address
	}
	if cfg.token != "" {
		consulCfg.Token = cfg.token
	}
	if cfg.timeout > 0 {
		consulCfg.WaitTime = cfg.timeout
	}

	cli, err := api.NewClient(consulCfg)
	if err != nil {
		return nil, fmt.Errorf("consul source: create client: %w", err)
	}

	return &Reader{
		client: kvClient{kv: cli.KV()},
		closer: nil, // api.Client has no explicit Close
		prefix: cfg.prefix,
		values: make(map[string]string),
		indexs: make(map[string]uint64),
	}, nil
}

// resolveKey prepends the configured prefix to the user-supplied key.
func (r *Reader) resolveKey(key string) string {
	return r.prefix + key
}

// Load fetches the value from Consul's KV store and returns it as a string.
// Context cancellation propagates to the underlying request via QueryOptions.
//
// A nil KVPair (key not found) is reported as a wrapped [ErrNotFound].
// Other errors are wrapped with the key for easier debugging.
func (r *Reader) Load(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	consulKey := r.resolveKey(key)
	q := &api.QueryOptions{}
	q = q.WithContext(ctx)

	pair, _, err := r.client.Get(consulKey, q)
	if err != nil {
		return "", fmt.Errorf("consul source: get %q: %w", consulKey, err)
	}

	if pair == nil {
		return "", fmt.Errorf("%w: %s", ErrNotFound, consulKey)
	}

	val := string(pair.Value)

	r.mu.Lock()
	r.values[key] = val
	r.indexs[key] = pair.ModifyIndex
	r.mu.Unlock()

	return val, nil
}

// Close releases the underlying Consul client resources, if any.
func (r *Reader) Close() error {
	if r.closer != nil {
		return r.closer.Close()
	}
	return nil
}

// IsNotFound reports whether err represents a "key not found" response from
// Consul. Equivalent to errors.Is(err, ErrNotFound).
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
