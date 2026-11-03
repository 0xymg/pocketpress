package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// QuickCheck runs SQLite's PRAGMA quick_check and reports whether the database
// is structurally sound. quick_check is the faster cousin of integrity_check:
// it verifies page structure and index consistency but skips the (much
// slower) UNIQUE/NOT NULL constraint re-validation, which is the right
// trade-off for a startup gate. A healthy database returns a single row "ok";
// anything else is treated as corruption and returned as an error naming the
// first reported problem(s).
//
// The limit argument bounds how many errors quick_check reports before giving
// up (PRAGMA quick_check(N)); pass a small positive value (e.g. 1) at startup
// so a corrupt database fails fast rather than scanning every damaged page.
func QuickCheck(ctx context.Context, db *sql.DB, limit int) error {
	if limit < 1 {
		limit = 1
	}
	// quick_check(N) does not accept a bound parameter, so the integer is
	// formatted directly into the pragma. limit is an internal, non-negative
	// int (never request-derived), so this is not an injection surface.
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA quick_check(%d)", limit))
	if err != nil {
		return fmt.Errorf("run quick_check: %w", err)
	}
	defer rows.Close()

	var results []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return fmt.Errorf("scan quick_check row: %w", err)
		}
		results = append(results, line)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate quick_check rows: %w", err)
	}

	return interpretQuickCheck(results)
}

// interpretQuickCheck maps the rows PRAGMA quick_check returns to a nil (sound)
// or non-nil (corrupt) result. A sound database returns exactly one "ok" row;
// any other output is corruption. It is separated from the query so the
// corruption branch can be unit-tested without fabricating a corrupt database
// file (which SQLite's driver does not reliably surface deterministically).
func interpretQuickCheck(results []string) error {
	if len(results) == 1 && results[0] == "ok" {
		return nil
	}
	if len(results) == 0 {
		// quick_check always returns at least one row; no rows means we could
		// not read the result, which we treat as a failure rather than "ok".
		return fmt.Errorf("database integrity check returned no result")
	}
	return fmt.Errorf("database integrity check failed: %s", strings.Join(results, "; "))
}
