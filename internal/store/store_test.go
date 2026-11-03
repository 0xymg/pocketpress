package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketpress/pocketpress/internal/store"
)

// TestOpenCreatesDatabaseFileAndSetsPragmas confirms Open creates
// <dataDir>/pocketpress.db and applies the three pragmas PocketPress
// requires: foreign_keys ON, journal_mode WAL, and a 5000ms busy_timeout.
func TestOpenCreatesDatabaseFileAndSetsPragmas(t *testing.T) {
	dir := t.TempDir()

	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			t.Errorf("store.Close() error = %v", err)
		}
	}()

	dbPath := filepath.Join(dir, store.DBFileName)
	if info, err := os.Stat(dbPath); err != nil || info.IsDir() {
		t.Fatalf("expected database file at %q, stat error = %v", dbPath, err)
	}

	var foreignKeys int
	if err := st.DB().QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("query foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d, want 1 (ON)", foreignKeys)
	}

	var journalMode string
	if err := st.DB().QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode pragma: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("PRAGMA journal_mode = %q, want %q", journalMode, "wal")
	}

	var busyTimeout int
	if err := st.DB().QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout pragma: %v", err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("PRAGMA busy_timeout = %d, want 5000", busyTimeout)
	}
}

// TestOpenCreatesMissingDataDir confirms Open creates dataDir itself when
// it doesn't already exist (e.g. a brand new PP_DATA_DIR), rather than
// requiring the caller to mkdir first.
func TestOpenCreatesMissingDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")

	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			t.Errorf("store.Close() error = %v", err)
		}
	}()

	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("expected data dir %q to be created, stat error = %v", dir, err)
	}
}
