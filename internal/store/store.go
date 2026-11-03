// Package store is PocketPress's SQLite persistence layer: opening the
// database with the pragmas PocketPress requires, running the embedded
// schema migrations (migrate.go), and the per-table repositories used by
// the rest of the application (users.go, sessions.go, settings.go).
//
// The storage engine is an internal implementation detail. Nothing outside
// this package (and the /api/v1 layer built on top of it) should depend on
// SQLite specifics; that keeps the promise in the development plan that
// the internal schema can change without breaking the public API.
package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DBFileName is the SQLite database file created inside the configured
// data directory.
const DBFileName = "pocketpress.db"

// dataDirPerm is the permission used when Open creates dataDir if it does
// not already exist.
const dataDirPerm = 0o755

// Store wraps the SQLite connection pool shared by every PocketPress
// repository.
type Store struct {
	db *sql.DB
}

// DB returns the underlying *sql.DB, for repositories and the migration
// runner.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Close closes the underlying database connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}

// Open opens (creating if necessary) <dataDir>/pocketpress.db with the
// pragmas PocketPress requires:
//
//   - journal_mode = WAL, for read concurrency and crash-safe writes.
//   - foreign_keys = ON, since SQLite does not enforce foreign keys by
//     default and PocketPress relies on ON DELETE CASCADE/RESTRICT/SET NULL
//     behavior defined in the schema.
//   - busy_timeout = 5000ms, so a connection briefly waits for the SQLite
//     writer lock instead of failing immediately with "database is locked".
//
// SQLite allows only one writer at a time; the pool is deliberately capped
// at a single open connection (SetMaxOpenConns(1)) so PocketPress never
// lets two goroutines contend for the writer lock at the driver level —
// simple and boring rather than tuning a larger pool around SQLite's
// single-writer constraint.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, dataDirPerm); err != nil {
		return nil, fmt.Errorf("create data dir %q: %w", dataDir, err)
	}

	dbPath := filepath.Join(dataDir, DBFileName)

	dsn := "file:" + filepath.ToSlash(dbPath) + "?" + url.Values{
		"_pragma": {"busy_timeout(5000)", "foreign_keys(1)", "journal_mode(WAL)"},
	}.Encode()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database %q: %w", dbPath, err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open sqlite database %q: %w", dbPath, err)
	}

	return &Store{db: db}, nil
}
