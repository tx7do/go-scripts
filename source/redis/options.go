package redis

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Option configures a [Reader]. Pass to [New].
type Option func(*configOptions)

// configOptions is the accumulator for Option values.
type configOptions struct {
	addr     string
	password string
	username string
	db       int
	prefix   string
	client   redisCmdable // for tests; when non-nil a real client is not created
	closer   io.Closer    // optional closer for the test client; may be nil
}

// redisCmdable is the subset of redis.Cmdable this package depends on. Defining
// it as an interface lets tests substitute a fake without spinning up a real
// Redis server.
type redisCmdable interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	Ping(ctx context.Context) *redis.StatusCmd
}

// WithAddr sets the Redis server address (default "localhost:6379").
func WithAddr(addr string) Option {
	return func(o *configOptions) { o.addr = addr }
}

// WithPassword sets the Redis password for AUTH.
func WithPassword(password string) Option {
	return func(o *configOptions) { o.password = password }
}

// WithUsername sets the Redis username (Redis ACL, since Redis 6.0).
func WithUsername(username string) Option {
	return func(o *configOptions) { o.username = username }
}

// WithDB selects the Redis logical database index (default 0).
func WithDB(db int) Option {
	return func(o *configOptions) { o.db = db }
}

// WithPrefix sets a key prefix that is transparently prepended to every key
// before it is resolved against Redis. Useful when all scripts share a
// common namespace (e.g. WithPrefix("scripts:lua:")).
//
// Leading slashes are stripped; no other normalization is applied so callers
// can use any separator (":", "/", etc.).
func WithPrefix(prefix string) Option {
	return func(o *configOptions) {
		prefix = strings.TrimPrefix(prefix, "/")
		o.prefix = prefix
	}
}
