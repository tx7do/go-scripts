package database

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Option configures a [Reader]. Pass to [New].
type Option func(*configOptions)

// configOptions is the accumulator for Option values.
type configOptions struct {
	driver          string        // database driver: "mysql", "postgres", "sqlite3", etc.
	dsn             string        // data source name
	db              *sql.DB       // pre-existing *sql.DB (for shared connection pools)
	table           string        // table name (default "scripts")
	keyColumn       string        // key column (default "name")
	valueColumn     string        // value column (default "content")
	checksumColumn  string        // checksum column for change detection (default "updated_at")
	query           string        // custom SQL query (overrides auto-generated)
	prefix          string        // key prefix
	pollInterval    time.Duration // polling interval for Watch (default 10s)
	maxOpenConns    int           // max open connections
	maxIdleConns    int           // max idle connections
	connMaxLifetime time.Duration // connection max lifetime

	// For test injection
	api dbAPI
}

// dbAPI is the subset of database operations this package depends on. Defining
// it as an interface lets tests substitute a fake without spinning up a real
// database server.
type dbAPI interface {
	// GetScript retrieves the script value and its change-detection checksum
	// for the given key. Returns ErrNotFound if the key does not exist.
	GetScript(ctx context.Context, key string) (value string, checksum string, err error)

	// Close releases underlying database resources.
	Close() error
}

// ioCloser is the minimal close interface.
type ioCloser interface {
	Close() error
}

// WithDriver sets the database driver name (e.g. "mysql", "postgres", "sqlite3").
// Must be used together with WithDSN unless WithDB is used.
func WithDriver(driver string) Option {
	return func(o *configOptions) { o.driver = driver }
}

// WithDSN sets the data source name for the database connection.
// Must be used together with WithDriver unless WithDB is used.
func WithDSN(dsn string) Option {
	return func(o *configOptions) { o.dsn = dsn }
}

// WithDB injects a pre-existing *sql.DB instance. When set, WithDriver and
// WithDSN are ignored. The Reader will NOT close the DB on Close().
func WithDB(db *sql.DB) Option {
	return func(o *configOptions) { o.db = db }
}

// WithTable sets the table name that stores scripts (default "scripts").
func WithTable(table string) Option {
	return func(o *configOptions) { o.table = table }
}

// WithKeyColumn sets the column name that identifies the script (default "name").
func WithKeyColumn(col string) Option {
	return func(o *configOptions) { o.keyColumn = col }
}

// WithValueColumn sets the column name that stores the script content
// (default "content").
func WithValueColumn(col string) Option {
	return func(o *configOptions) { o.valueColumn = col }
}

// WithChecksumColumn sets the column used for change detection (default
// "updated_at"). This should be a column that changes whenever the row is
// modified (e.g., updated_at, version, checksum, etag).
func WithChecksumColumn(col string) Option {
	return func(o *configOptions) { o.checksumColumn = col }
}

// WithQuery sets a custom SQL query that overrides the auto-generated one.
// The query must return exactly two columns: value first, checksum second.
// The key is passed as the first positional parameter (? or $1).
//
// Example for PostgreSQL:
//
//	WithQuery("SELECT content, updated_at FROM scripts WHERE name = $1")
//
// Example for MySQL / SQLite:
//
//	WithQuery("SELECT content, updated_at FROM scripts WHERE name = ?")
func WithQuery(query string) Option {
	return func(o *configOptions) { o.query = query }
}

// WithPrefix sets a key prefix that is transparently prepended to every key
// before it is resolved against the database. Useful when all scripts share a
// common namespace (e.g. WithPrefix("scripts/lua/")).
//
// Leading slashes are stripped; no other normalization is applied.
func WithPrefix(prefix string) Option {
	return func(o *configOptions) {
		prefix = strings.TrimPrefix(prefix, "/")
		o.prefix = prefix
	}
}

// WithPollInterval sets the polling interval for Watch (default 10s).
func WithPollInterval(d time.Duration) Option {
	return func(o *configOptions) { o.pollInterval = d }
}

// WithMaxOpenConns sets the maximum number of open connections.
func WithMaxOpenConns(n int) Option {
	return func(o *configOptions) { o.maxOpenConns = n }
}

// WithMaxIdleConns sets the maximum number of idle connections.
func WithMaxIdleConns(n int) Option {
	return func(o *configOptions) { o.maxIdleConns = n }
}

// WithConnMaxLifetime sets the maximum lifetime of a connection.
func WithConnMaxLifetime(d time.Duration) Option {
	return func(o *configOptions) { o.connMaxLifetime = d }
}
