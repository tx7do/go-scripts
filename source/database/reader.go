// Package database provides a [source.Reader] implementation that reads
// scripts from any SQL database via the standard database/sql package.
//
// Supported databases include MySQL, PostgreSQL, SQLite, SQL Server, and any
// driver registered with database/sql.
//
// Construction:
//
//	src, err := database.New(ctx,
//	    database.WithDriver("mysql"),
//	    database.WithDSN("user:pass@tcp(localhost:3306)/scripts"),
//	    database.WithTable("scripts"),
//	    database.WithKeyColumn("name"),
//	    database.WithValueColumn("content"),
//	    database.WithChecksumColumn("updated_at"),
//	)
//
// Hot-reload detection uses checksum polling: the Watcher periodically re-queries
// the checksum column and compares it with the value recorded at the last Load.
package database

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/tx7do/go-scripts/source"
)

// Reader reads scripts from a SQL database.
//
// All exported methods are safe for concurrent use. Reader implements the
// [source.ReadWatcher] interface.
type Reader struct {
	api     dbAPI // database operations (real or fake)
	prefix  string
	ownsAPI bool // true if this Reader should close the API on Close()

	pollInterval time.Duration

	mu        sync.RWMutex
	values    map[string]string // key -> last loaded value
	checksums map[string]string // key -> last checksum (for change detection)
	closed    bool
}

// Compile-time assertion: *Reader implements source.Reader.
var _ source.Reader = (*Reader)(nil)

// Compile-time assertion: *Reader also implements source.ReadWatcher.
var _ source.ReadWatcher = (*Reader)(nil)

// defaultPollInterval is the default polling interval for Watch.
const defaultPollInterval = 10 * time.Second

// withAPI is an internal option used by tests to inject a fake [dbAPI].
// Not exported.
func withAPI(api dbAPI) Option {
	return func(o *configOptions) {
		o.api = api
	}
}

// New creates a database-backed [Reader].
//
// At minimum, either:
//   - WithDriver + WithDSN (the Reader opens and owns the connection)
//   - WithDB (the Reader uses a pre-existing *sql.DB and will not close it)
//
// must be supplied. All other settings are optional with sensible defaults.
func New(_ context.Context, opts ...Option) (*Reader, error) {
	cfg := &configOptions{
		pollInterval: defaultPollInterval,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Test-injected dbAPI bypasses real database entirely.
	if cfg.api != nil {
		return &Reader{
			api:          cfg.api,
			prefix:       cfg.prefix,
			ownsAPI:      false,
			pollInterval: cfg.pollInterval,
			values:       make(map[string]string),
			checksums:    make(map[string]string),
		}, nil
	}

	// Create the real database operator.
	op, err := newSqlDB(cfg)
	if err != nil {
		return nil, err
	}

	return &Reader{
		api:          op,
		prefix:       cfg.prefix,
		ownsAPI:      true,
		pollInterval: cfg.pollInterval,
		values:       make(map[string]string),
		checksums:    make(map[string]string),
	}, nil
}

// resolveKey prepends the configured prefix to the user-supplied key.
func (r *Reader) resolveKey(key string) string {
	return r.prefix + key
}

// Load fetches the script value from the database and returns it as a string.
// Context cancellation propagates to the underlying query.
//
// A missing row (sql.ErrNoRows) is reported as a wrapped [ErrNotFound].
// Other errors are wrapped with the key for easier debugging.
func (r *Reader) Load(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	resolved := r.resolveKey(key)
	val, checksum, err := r.api.GetScript(ctx, resolved)
	if err != nil {
		return "", err
	}

	r.mu.Lock()
	r.values[key] = val
	r.checksums[key] = checksum
	r.mu.Unlock()

	return val, nil
}

// Close releases the underlying database connection if this Reader owns it.
func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true

	if r.ownsAPI && r.api != nil {
		return r.api.Close()
	}
	return nil
}

// IsNotFound reports whether err represents a "key not found" response from
// the database. Equivalent to errors.Is(err, ErrNotFound).
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
