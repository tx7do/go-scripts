package etcd

import (
	"context"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// Option configures a [Reader]. Pass to [New].
type Option func(*configOptions)

// configOptions is the accumulator for Option values.
type configOptions struct {
	endpoints []string
	username  string
	password  string
	prefix    string
	timeout   time.Duration
	client    clientAPI // for tests; when non-nil a real client is not created
	closer    ioCloser  // optional closer for the test client; may be nil
}

// ioCloser is the minimal close interface.
type ioCloser interface {
	Close() error
}

// clientAPI is the subset of *clientv3.Client this package depends on. Defining
// it as an interface lets tests substitute a fake without spinning up a real
// etcd server.
type clientAPI interface {
	Get(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error)
	Watch(ctx context.Context, key string, opts ...clientv3.OpOption) clientv3.WatchChan
}

// WithEndpoints sets the etcd server endpoints (default ["localhost:2379"]).
func WithEndpoints(endpoints ...string) Option {
	return func(o *configOptions) { o.endpoints = endpoints }
}

// WithUsername sets the etcd username for authentication.
func WithUsername(username string) Option {
	return func(o *configOptions) { o.username = username }
}

// WithPassword sets the etcd password for authentication.
func WithPassword(password string) Option {
	return func(o *configOptions) { o.password = password }
}

// WithPrefix sets a key prefix that is transparently prepended to every key
// before it is resolved against etcd. Useful when all scripts share a common
// namespace (e.g. WithPrefix("scripts/lua/")).
//
// Leading slashes are stripped; no other normalization is applied.
func WithPrefix(prefix string) Option {
	return func(o *configOptions) {
		prefix = strings.TrimPrefix(prefix, "/")
		o.prefix = prefix
	}
}

// WithTimeout sets the etcd client timeout (default 5s).
func WithTimeout(timeout time.Duration) Option {
	return func(o *configOptions) { o.timeout = timeout }
}
