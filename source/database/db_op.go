package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// sqlDB implements [dbAPI] using the standard database/sql package.
type sqlDB struct {
	db    *sql.DB
	query string // pre-built query (or custom query from config)
	owned bool   // true if this instance opened the DB and should Close it
}

// newSqlDB creates a sqlDB from the given configuration.
// Returns an error if the driver is not registered.
func newSqlDB(cfg *configOptions) (*sqlDB, error) {
	// Build the query (auto-generated or custom).
	query, err := buildQuery(cfg)
	if err != nil {
		return nil, err
	}

	// Case 1: pre-existing *sql.DB injected via WithDB.
	if cfg.db != nil {
		return &sqlDB{
			db:    cfg.db,
			query: query,
			owned: false,
		}, nil
	}

	// Case 2: open a new connection.
	if cfg.driver == "" || cfg.dsn == "" {
		return nil, errors.New("database source: WithDriver and WithDSN are required (or use WithDB)")
	}

	db, err := sql.Open(cfg.driver, cfg.dsn)
	if err != nil {
		return nil, fmt.Errorf("database source: open %q: %w", cfg.driver, err)
	}

	// Apply connection pool settings.
	if cfg.maxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.maxOpenConns)
	}
	if cfg.maxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.maxIdleConns)
	}
	if cfg.connMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.connMaxLifetime)
	}

	return &sqlDB{
		db:    db,
		query: query,
		owned: true,
	}, nil
}

// GetScript implements dbAPI. It executes the pre-built query and returns the
// script value and checksum. Returns ErrNotFound when the row is absent.
func (s *sqlDB) GetScript(ctx context.Context, key string) (string, string, error) {
	var value, checksum string
	err := s.db.QueryRowContext(ctx, s.query, key).Scan(&value, &checksum)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", fmt.Errorf("%w: %s", ErrNotFound, key)
		}
		return "", "", fmt.Errorf("database source: query %q: %w", key, err)
	}
	return value, checksum, nil
}

// Close releases the underlying database connection if this instance owns it.
func (s *sqlDB) Close() error {
	if s.owned && s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Ensure sqlDB implements dbAPI at compile time.
var _ dbAPI = (*sqlDB)(nil)

// buildQuery constructs the SQL query from either a custom query or
// auto-generated using table and column names.
func buildQuery(cfg *configOptions) (string, error) {
	// Custom query takes precedence.
	if cfg.query != "" {
		return cfg.query, nil
	}

	// Apply defaults.
	table := cfg.table
	if table == "" {
		table = "scripts"
	}
	keyCol := cfg.keyColumn
	if keyCol == "" {
		keyCol = "name"
	}
	valueCol := cfg.valueColumn
	if valueCol == "" {
		valueCol = "content"
	}
	checksumCol := cfg.checksumColumn
	if checksumCol == "" {
		checksumCol = "updated_at"
	}

	// Build the query with ? placeholder.
	// Most Go drivers support ? (MySQL, SQLite, pgx/v5).
	return fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s = ?",
		quoteIdent(valueCol), quoteIdent(checksumCol),
		quoteIdent(table), quoteIdent(keyCol)), nil
}

// quoteIdent wraps an identifier in double quotes for SQL safety.
// This handles the common case of reserved keywords and mixed-case identifiers.
func quoteIdent(name string) string {
	return "\"" + name + "\""
}
