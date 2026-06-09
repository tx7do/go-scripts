package consul

import (
	"strings"
	"time"

	"github.com/hashicorp/consul/api"
)

// Option configures a [Reader]. Pass to [New].
type Option func(*configOptions)

// ioCloser is the minimal close interface.
type ioCloser interface {
	Close() error
}

// configOptions is the accumulator for Option values.
type configOptions struct {
	address string
	token   string
	prefix  string
	timeout time.Duration
	client  consulAPI // for tests; when non-nil a real client is not created
	closer  ioCloser  // optional closer for the test client; may be nil
}

// consulAPI is the subset of *api.Client's KV this package depends on. Defining
// it as an interface lets tests substitute a fake without spinning up a real
// Consul agent.
type consulAPI interface {
	Get(key string, q *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error)
	List(prefix string, q *api.QueryOptions) (api.KVPairs, *api.QueryMeta, error)
}

// kvClient adapts *api.KV to consulAPI.
type kvClient struct{ kv *api.KV }

func (c kvClient) Get(key string, q *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error) {
	return c.kv.Get(key, q)
}

func (c kvClient) List(prefix string, q *api.QueryOptions) (api.KVPairs, *api.QueryMeta, error) {
	return c.kv.List(prefix, q)
}

// WithAddress sets the Consul agent address (default "127.0.0.1:8500").
func WithAddress(address string) Option {
	return func(o *configOptions) { o.address = address }
}

// WithToken sets the ACL token used for authentication.
func WithToken(token string) Option {
	return func(o *configOptions) { o.token = token }
}

// WithPrefix sets a key prefix that is transparently prepended to every key
// before it is resolved against Consul. Useful when all scripts share a common
// namespace (e.g. WithPrefix("scripts/lua/")).
//
// Leading slashes are stripped; no other normalization is applied.
func WithPrefix(prefix string) Option {
	return func(o *configOptions) {
		prefix = strings.TrimPrefix(prefix, "/")
		o.prefix = prefix
	}
}

// WithTimeout sets the Consul client HTTP timeout (default 5s). This only
// applies when the Reader constructs its own client (i.e. not in tests).
func WithTimeout(timeout time.Duration) Option {
	return func(o *configOptions) { o.timeout = timeout }
}
