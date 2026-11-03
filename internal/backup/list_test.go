package backup_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/backup"
	"github.com/pocketpress/pocketpress/internal/store"
)

// prepareSeededStore opens (creating) and migrates a store at dataDir, then
// closes it, so backup.Create has something valid to vacuum.
func prepareSeededStore(t *testing.T, dataDir string) {
	t.Helper()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	if _, err := store.Up(context.Background(), st.DB()); err != nil {
		t.Fatalf("store.Up() error = %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("store.Close() error = %v", err)
	}
}

// TestListEmptyDirectory confirms List returns an empty, non-error result
// when <dataDir>/backups does not exist yet (the friendly-message case the
// CLI handles).
func TestListEmptyDirectory(t *testing.T) {
	dataDir := t.TempDir()

	entries, err := backup.List(dataDir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("List() = %d entries, want 0", len(entries))
	}
}

// TestListReturnsCreatedArchivesNewestFirst creates two backups a moment
// apart and confirms List reports both, using the manifest's created_at,
// ordered newest first.
func TestListReturnsCreatedArchivesNewestFirst(t *testing.T) {
	dataDir := t.TempDir()
	prepareSeededStore(t, dataDir)

	first, err := backup.Create(context.Background(), backup.CreateOptions{
		DataDir: dataDir, Out: filepath.Join(dataDir, backup.BackupsDirName, "first.zip"),
		Version: testVersion, StorageDriver: testStorageDriver,
	})
	if err != nil {
		t.Fatalf("Create() (first) error = %v", err)
	}

	// Force a distinguishable created_at without a real sleep: back-date
	// the first archive's manifest by rewriting its mtime is not enough
	// (List prefers manifest content), so instead just create the second
	// archive normally — both timestamps are UTC "now" but manifests are
	// generated independently. To make ordering deterministic regardless of
	// clock resolution, nudge the first archive's file mtime back; List
	// only falls back to mtime when the manifest is unreadable, so what we
	// actually need is enough real elapsed time between the two Creates.
	time.Sleep(1100 * time.Millisecond)

	second, err := backup.Create(context.Background(), backup.CreateOptions{
		DataDir: dataDir, Out: filepath.Join(dataDir, backup.BackupsDirName, "second.zip"),
		Version: testVersion, StorageDriver: testStorageDriver,
	})
	if err != nil {
		t.Fatalf("Create() (second) error = %v", err)
	}

	entries, err := backup.List(dataDir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("List() = %d entries, want 2", len(entries))
	}

	if entries[0].Path != second.Path || entries[1].Path != first.Path {
		t.Fatalf("List() order = [%s, %s], want [%s, %s] (newest first)",
			entries[0].Name, entries[1].Name, filepath.Base(second.Path), filepath.Base(first.Path))
	}
	for i, e := range entries {
		if !e.FromManifest {
			t.Errorf("entries[%d].FromManifest = false, want true (both archives have a manifest)", i)
		}
	}
	if entries[0].Size != second.Size {
		t.Errorf("entries[0].Size = %d, want %d", entries[0].Size, second.Size)
	}
}

// TestListFallsBackToModTimeForUnreadableManifest confirms a *.zip file in
// the backups directory that is not a valid PocketPress archive (e.g.
// dropped in by hand) is still listed, using its file mtime instead of
// failing the whole listing.
func TestListFallsBackToModTimeForUnreadableManifest(t *testing.T) {
	dataDir := t.TempDir()
	backupsDir := filepath.Join(dataDir, backup.BackupsDirName)
	if err := os.MkdirAll(backupsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	notAZip := filepath.Join(backupsDir, "not-really-a-zip.zip")
	if err := os.WriteFile(notAZip, []byte("not a zip file"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	entries, err := backup.List(dataDir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("List() = %d entries, want 1", len(entries))
	}
	if entries[0].FromManifest {
		t.Errorf("entries[0].FromManifest = true, want false (invalid zip has no manifest)")
	}
	if entries[0].CreatedAt.IsZero() {
		t.Errorf("entries[0].CreatedAt is zero, want the file's mod time")
	}
}
