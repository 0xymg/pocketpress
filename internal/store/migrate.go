package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/pocketpress/pocketpress/migrations"
)

// createSchemaMigrationsTableSQL creates the bookkeeping table the runner
// uses to track which embedded migrations have been applied. It is owned
// entirely by the runner (not by a numbered migration file) since the
// runner needs it to exist before it can even determine which numbered
// migrations are pending.
const createSchemaMigrationsTableSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version    TEXT PRIMARY KEY,
	applied_at TEXT NOT NULL
)`

// Up applies every embedded migration (migrations.All) that is not yet
// recorded in schema_migrations, in ascending version order. Each
// migration's up script runs inside its own transaction alongside the
// INSERT that records it as applied, so a failure partway through leaves
// only fully-applied migrations recorded.
//
// Up returns the versions it applied, in the order applied. Calling Up
// again on an up-to-date database is a no-op: it returns a nil slice.
func Up(ctx context.Context, db *sql.DB) ([]string, error) {
	if err := ensureMigrationsTable(ctx, db); err != nil {
		return nil, err
	}

	all, err := migrations.All()
	if err != nil {
		return nil, err
	}

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return nil, err
	}

	var newlyApplied []string
	for _, m := range all {
		if applied[m.Version] {
			continue
		}

		if err := applyMigration(ctx, db, m.Version, m.Up); err != nil {
			return newlyApplied, fmt.Errorf("apply migration %s: %w", m.Version, err)
		}
		newlyApplied = append(newlyApplied, m.Version)
	}

	return newlyApplied, nil
}

// Down rolls back exactly one migration: the most recently applied
// version, running its embedded down script and deleting its
// schema_migrations row inside a single transaction.
//
// Down returns the version it rolled back, or "" if no migrations were
// applied (nothing to do).
func Down(ctx context.Context, db *sql.DB) (string, error) {
	if err := ensureMigrationsTable(ctx, db); err != nil {
		return "", err
	}

	all, err := migrations.All()
	if err != nil {
		return "", err
	}

	latest, err := latestAppliedVersion(ctx, db)
	if err != nil {
		return "", err
	}
	if latest == "" {
		return "", nil
	}

	var target *migrations.Migration
	for i := range all {
		if all[i].Version == latest {
			target = &all[i]
			break
		}
	}
	if target == nil {
		return "", fmt.Errorf("no embedded migration found for applied version %q", latest)
	}

	if err := revertMigration(ctx, db, target.Version, target.Down); err != nil {
		return "", fmt.Errorf("revert migration %s: %w", target.Version, err)
	}

	return target.Version, nil
}

// ensureMigrationsTable creates schema_migrations if it does not already
// exist. It is safe to call on every Up/Down invocation.
func ensureMigrationsTable(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, createSchemaMigrationsTableSQL); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}
	return nil
}

// appliedVersions returns the set of migration versions already recorded
// in schema_migrations.
func appliedVersions(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan schema_migrations row: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations rows: %w", err)
	}

	return applied, nil
}

// latestAppliedVersion returns the highest (lexicographically, which is
// also chronologically since versions are zero-padded and monotonic)
// recorded migration version, or "" if none are recorded.
func latestAppliedVersion(ctx context.Context, db *sql.DB) (string, error) {
	var version string
	err := db.QueryRowContext(ctx, `SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&version)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("query latest schema_migrations version: %w", err)
	}
	return version, nil
}

// applyMigration runs upSQL and records version as applied, inside a
// single transaction.
func applyMigration(ctx context.Context, db *sql.DB, version, upSQL string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, upSQL); err != nil {
		return fmt.Errorf("execute up script: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		version, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("record migration version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// revertMigration runs downSQL and removes version's schema_migrations
// row, inside a single transaction.
func revertMigration(ctx context.Context, db *sql.DB, version, downSQL string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, downSQL); err != nil {
		return fmt.Errorf("execute down script: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = ?`, version); err != nil {
		return fmt.Errorf("remove migration version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
