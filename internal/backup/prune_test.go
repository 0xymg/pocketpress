package backup_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/backup"
)

// writeZipFixture creates a minimal (possibly empty) .zip file at path so
// List picks it up. Content does not need to be a valid PocketPress archive
// for prune tests — List falls back to mtime when the manifest is missing.
func writeZipFixture(t *testing.T, path string, modTime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("PK\x05\x06"+strings.Repeat("\x00", 18)), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("Chtimes(%q): %v", path, err)
	}
}

// TestPruneKeepsNewestOnly confirms Prune deletes older archives and leaves
// exactly keep newest ones.
func TestPruneKeepsNewestOnly(t *testing.T) {
	dataDir := t.TempDir()
	backups := filepath.Join(dataDir, backup.BackupsDirName)
	now := time.Now().UTC()

	oldest := filepath.Join(backups, "old.zip")
	mid := filepath.Join(backups, "mid.zip")
	newest := filepath.Join(backups, "new.zip")
	writeZipFixture(t, oldest, now.Add(-3*time.Hour))
	writeZipFixture(t, mid, now.Add(-2*time.Hour))
	writeZipFixture(t, newest, now.Add(-time.Hour))

	result, err := backup.Prune(dataDir, 2)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if result.Kept != 2 || result.Removed != 1 {
		t.Fatalf("Prune() = %+v, want Kept=2 Removed=1", result)
	}
	if len(result.RemovedPaths) != 1 || result.RemovedPaths[0] != oldest {
		t.Fatalf("RemovedPaths = %v, want [%q]", result.RemovedPaths, oldest)
	}
	if _, err := os.Stat(oldest); !os.IsNotExist(err) {
		t.Fatalf("oldest archive still present: %v", err)
	}
	for _, path := range []string{mid, newest} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %q to remain: %v", path, err)
		}
	}
}

// TestPruneNoOpWhenUnderKeep confirms nothing is deleted when the archive
// count is already within the retention limit.
func TestPruneNoOpWhenUnderKeep(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, backup.BackupsDirName, "only.zip")
	writeZipFixture(t, path, time.Now().UTC())

	result, err := backup.Prune(dataDir, 7)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if result.Removed != 0 || result.Kept != 1 {
		t.Fatalf("Prune() = %+v, want Kept=1 Removed=0", result)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("archive was deleted unexpectedly: %v", err)
	}
}

// TestPruneRejectsInvalidKeep is the retention safety boundary: keep < 1
// must fail rather than deleting every archive.
func TestPruneRejectsInvalidKeep(t *testing.T) {
	_, err := backup.Prune(t.TempDir(), 0)
	if err == nil {
		t.Fatal("Prune(keep=0) = nil, want an error")
	}
	if !strings.Contains(err.Error(), "at least 1") {
		t.Fatalf("error = %q, want it to mention the keep minimum", err.Error())
	}
}

// TestPrunePermissionBoundaryOnlyTouchesBackupZips confirms Prune never
// deletes non-zip files in the backups directory, nested directories, or
// files outside <dataDir>/backups — the filesystem permission/safety
// boundary for automatic retention.
func TestPrunePermissionBoundaryOnlyTouchesBackupZips(t *testing.T) {
	dataDir := t.TempDir()
	backups := filepath.Join(dataDir, backup.BackupsDirName)
	now := time.Now().UTC()

	keepZip := filepath.Join(backups, "keep.zip")
	dropZip := filepath.Join(backups, "drop.zip")
	writeZipFixture(t, keepZip, now)
	writeZipFixture(t, dropZip, now.Add(-time.Hour))

	sidecar := filepath.Join(backups, "notes.txt")
	if err := os.WriteFile(sidecar, []byte("do not delete"), 0o644); err != nil {
		t.Fatalf("WriteFile(sidecar): %v", err)
	}

	nestedDir := filepath.Join(backups, "nested")
	nestedZip := filepath.Join(nestedDir, "hidden.zip")
	writeZipFixture(t, nestedZip, now.Add(-2*time.Hour))

	outside := filepath.Join(dataDir, "outside.zip")
	if err := os.WriteFile(outside, []byte("PK\x05\x06"+strings.Repeat("\x00", 18)), 0o644); err != nil {
		t.Fatalf("WriteFile(outside): %v", err)
	}
	dbPath := filepath.Join(dataDir, backup.DBFileName)
	if err := os.WriteFile(dbPath, []byte("not-a-real-db"), 0o644); err != nil {
		t.Fatalf("WriteFile(db): %v", err)
	}

	result, err := backup.Prune(dataDir, 1)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if result.Removed != 1 || result.RemovedPaths[0] != dropZip {
		t.Fatalf("Prune() = %+v, want only %q removed", result, dropZip)
	}

	mustExist := []string{keepZip, sidecar, nestedZip, outside, dbPath}
	for _, path := range mustExist {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("safety boundary violated: %q missing after prune: %v", path, err)
		}
	}
	if _, err := os.Stat(dropZip); !os.IsNotExist(err) {
		t.Fatalf("drop.zip should have been pruned: %v", err)
	}
}
