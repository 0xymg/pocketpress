package store_test

import (
	"context"
	"testing"

	"github.com/pocketpress/pocketpress/internal/store"
)

// TestUpAppliesInitMigrationAndIsIdempotent confirms Up applies every
// embedded migration (0001_init, 0002_media, 0003_fts) in order on a fresh
// database, and that calling Up again applies nothing.
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
	wantApplied := []string{"0001_init", "0002_media", "0003_fts"}
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

	// First Down reverts 0003_fts only.
	reverted, err := store.Down(ctx, st.DB())
	if err != nil {
		t.Fatalf("store.Down() error = %v", err)
	}
	if reverted != "0003_fts" {
		t.Fatalf("store.Down() reverted = %q, want %q", reverted, "0003_fts")
	}
	assertTableMissing(t, st, "posts_fts")
	assertTableExists(t, st, "media")
	assertTableExists(t, st, "posts")

	// Second Down reverts 0002_media only: media goes away, posts (from
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

	// Third Down reverts 0001_init: posts goes away too.
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
		t.Fatalf("store.Down() (fourth call) error = %v", err)
	}
	if revertedAgain != "" {
		t.Fatalf("store.Down() (fourth call) reverted = %q, want \"\"", revertedAgain)
	}

	appliedAgain, err := store.Up(ctx, st.DB())
	if err != nil {
		t.Fatalf("store.Up() (after down) error = %v", err)
	}
	wantAgain := []string{"0001_init", "0002_media", "0003_fts"}
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
