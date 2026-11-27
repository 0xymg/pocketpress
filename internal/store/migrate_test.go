package store_test

import (
	"context"
	"testing"

	"github.com/pocketpress/pocketpress/internal/store"
)

// TestUpAppliesInitMigrationAndIsIdempotent confirms Up applies every
// embedded migration in order on a fresh database, and that calling Up again
// applies nothing.
func TestUpAppliesInitMigrationAndIsIdempotent(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer st.Close()

	ctx := context.Background()

	applied, err := store.Up(ctx, st.DB())
	if err != nil {
		t.Fatalf("store.Up() error = %v", err)
	}
	wantApplied := []string{"0001_init", "0002_media", "0003_fts", "0004_webhook_filters", "0005_author_profiles"}
	if len(applied) != len(wantApplied) {
		t.Fatalf("store.Up() applied = %v, want %v", applied, wantApplied)
	}
	for i := range wantApplied {
		if applied[i] != wantApplied[i] {
			t.Fatalf("store.Up() applied = %v, want %v", applied, wantApplied)
		}
	}

	assertTableExists(t, st, "posts")
	assertTableExists(t, st, "media")
	assertTableExists(t, st, "posts_fts")
	assertColumnExists(t, st, "webhooks", "filters")
	assertColumnExists(t, st, "users", "slug")
	assertColumnExists(t, st, "users", "bio")

	appliedAgain, err := store.Up(ctx, st.DB())
	if err != nil {
		t.Fatalf("store.Up() (second call) error = %v", err)
	}
	if len(appliedAgain) != 0 {
		t.Fatalf("store.Up() (second call) applied = %v, want none", appliedAgain)
	}
}

// TestUpDownUpCycle confirms Down reverts exactly one migration at a time —
// the most recently applied one first — undoing only the schema that
// migration created, and that Up can then be run again cleanly.
func TestUpDownUpCycle(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer st.Close()

	ctx := context.Background()

	if _, err := store.Up(ctx, st.DB()); err != nil {
		t.Fatalf("store.Up() error = %v", err)
	}
	assertTableExists(t, st, "posts")
	assertTableExists(t, st, "media")
	assertTableExists(t, st, "posts_fts")
	assertColumnExists(t, st, "webhooks", "filters")
	assertColumnExists(t, st, "users", "slug")

	// First Down reverts 0005_author_profiles only.
	reverted, err := store.Down(ctx, st.DB())
	if err != nil {
		t.Fatalf("store.Down() error = %v", err)
	}
	if reverted != "0005_author_profiles" {
		t.Fatalf("store.Down() reverted = %q, want %q", reverted, "0005_author_profiles")
	}
	assertColumnMissing(t, st, "users", "slug")
	assertColumnExists(t, st, "webhooks", "filters")

	// Second Down reverts 0004_webhook_filters only.
	reverted, err = store.Down(ctx, st.DB())
	if err != nil {
		t.Fatalf("store.Down() (filters) error = %v", err)
	}
	if reverted != "0004_webhook_filters" {
		t.Fatalf("store.Down() (filters) reverted = %q, want %q", reverted, "0004_webhook_filters")
	}
	assertColumnMissing(t, st, "webhooks", "filters")
	assertTableExists(t, st, "posts_fts")

	// Third Down reverts 0003_fts only.
	reverted, err = store.Down(ctx, st.DB())
	if err != nil {
		t.Fatalf("store.Down() (fts) error = %v", err)
	}
	if reverted != "0003_fts" {
		t.Fatalf("store.Down() (fts) reverted = %q, want %q", reverted, "0003_fts")
	}
	assertTableMissing(t, st, "posts_fts")
	assertTableExists(t, st, "media")
	assertTableExists(t, st, "posts")

	// Fourth Down reverts 0002_media only: media goes away, posts (from
	// 0001_init) is untouched.
	reverted, err = store.Down(ctx, st.DB())
	if err != nil {
		t.Fatalf("store.Down() (second migration) error = %v", err)
	}
	if reverted != "0002_media" {
		t.Fatalf("store.Down() (second migration) reverted = %q, want %q", reverted, "0002_media")
	}
	assertTableMissing(t, st, "media")
	assertTableExists(t, st, "posts")

	// Fifth Down reverts 0001_init: posts goes away too.
	reverted, err = store.Down(ctx, st.DB())
	if err != nil {
		t.Fatalf("store.Down() (third migration) error = %v", err)
	}
	if reverted != "0001_init" {
		t.Fatalf("store.Down() (third migration) reverted = %q, want %q", reverted, "0001_init")
	}
	assertTableMissing(t, st, "posts")

	// Down with nothing left applied is a no-op, not an error.
	revertedAgain, err := store.Down(ctx, st.DB())
	if err != nil {
		t.Fatalf("store.Down() (sixth call) error = %v", err)
	}
	if revertedAgain != "" {
		t.Fatalf("store.Down() (sixth call) reverted = %q, want \"\"", revertedAgain)
	}

	appliedAgain, err := store.Up(ctx, st.DB())
	if err != nil {
		t.Fatalf("store.Up() (after down) error = %v", err)
	}
	wantAgain := []string{"0001_init", "0002_media", "0003_fts", "0004_webhook_filters", "0005_author_profiles"}
	if len(appliedAgain) != len(wantAgain) {
		t.Fatalf("store.Up() (after down) applied = %v, want %v", appliedAgain, wantAgain)
	}
	for i := range wantAgain {
		if appliedAgain[i] != wantAgain[i] {
			t.Fatalf("store.Up() (after down) applied = %v, want %v", appliedAgain, wantAgain)
		}
	}
	assertTableExists(t, st, "posts")
	assertTableExists(t, st, "media")
	assertTableExists(t, st, "posts_fts")
	assertColumnExists(t, st, "webhooks", "filters")
	assertColumnExists(t, st, "users", "slug")
}

func assertTableExists(t *testing.T, st *store.Store, name string) {
	t.Helper()

	var got string
	err := st.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, name,
	).Scan(&got)
	if err != nil {
		t.Fatalf("expected table %q to exist: %v", name, err)
	}
}

func assertTableMissing(t *testing.T, st *store.Store, name string) {
	t.Helper()

	var count int
	err := st.DB().QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name,
	).Scan(&count)
	if err != nil {
		t.Fatalf("query sqlite_master for %q: %v", name, err)
	}
	if count != 0 {
		t.Fatalf("expected table %q to not exist, found %d", name, count)
	}
}

func assertColumnExists(t *testing.T, st *store.Store, table, column string) {
	t.Helper()
	rows, err := st.DB().Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if name == column {
			return
		}
	}
	t.Fatalf("expected column %s.%s to exist", table, column)
}

func assertColumnMissing(t *testing.T, st *store.Store, table, column string) {
	t.Helper()
	rows, err := st.DB().Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if name == column {
			t.Fatalf("expected column %s.%s to be missing", table, column)
		}
	}
}
