package backup_test

import (
	"archive/zip"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/pocketpress/pocketpress/internal/backup"
	"github.com/pocketpress/pocketpress/internal/store"
)

// TestRestoreRoundTrip is the plan's acceptance criterion end to end: seed
// one instance, back it up, restore the archive into a second, completely
// empty data directory, and confirm the seeded user, post and upload file
// bytes all survive.
func TestRestoreRoundTrip(t *testing.T) {
	sourceDir := t.TempDir()
	seed := seedInstance(t, sourceDir)

	created, err := backup.Create(context.Background(), backup.CreateOptions{
		DataDir:       sourceDir,
		Version:       testVersion,
		StorageDriver: testStorageDriver,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// A fresh, otherwise-empty second data directory — not even created
	// yet, to prove Restore creates it.
	targetDir := filepath.Join(t.TempDir(), "restored-instance")

	result, err := backup.Restore(backup.RestoreOptions{
		DataDir:     targetDir,
		ArchivePath: created.Path,
	})
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if result.PreRestoreDir != "" {
		t.Fatalf("Restore() into an empty dir set PreRestoreDir = %q, want \"\"", result.PreRestoreDir)
	}
	if result.UploadFiles != 1 {
		t.Fatalf("Restore() UploadFiles = %d, want 1", result.UploadFiles)
	}
	if result.Manifest.PocketPressVersion != testVersion {
		t.Errorf("Restore() Manifest.PocketPressVersion = %q, want %q", result.Manifest.PocketPressVersion, testVersion)
	}

	dbPath := filepath.Join(targetDir, store.DBFileName)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open(restored db) error = %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	restoredUser, err := store.GetUserByID(ctx, db, seed.userID)
	if err != nil {
		t.Fatalf("GetUserByID() on restored db error = %v", err)
	}
	if restoredUser.Email != seed.userEmail {
		t.Errorf("restored user email = %q, want %q", restoredUser.Email, seed.userEmail)
	}

	restoredPost, err := store.GetPostByID(ctx, db, seed.postID)
	if err != nil {
		t.Fatalf("GetPostByID() on restored db error = %v", err)
	}
	if restoredPost.Slug != seed.postSlug {
		t.Errorf("restored post slug = %q, want %q", restoredPost.Slug, seed.postSlug)
	}

	uploadPath := filepath.Join(targetDir, backup.UploadsDirName, seed.uploadRelKey)
	gotBytes, err := os.ReadFile(uploadPath)
	if err != nil {
		t.Fatalf("read restored upload %q error = %v", uploadPath, err)
	}
	if string(gotBytes) != string(seed.uploadBytes) {
		t.Errorf("restored upload bytes = %q, want %q", gotBytes, seed.uploadBytes)
	}
}

// TestRestoreRefusesWithoutForce confirms Restore refuses to overwrite an
// existing pocketpress.db unless Force is set, and leaves the existing
// database and uploads untouched.
func TestRestoreRefusesWithoutForce(t *testing.T) {
	sourceDir := t.TempDir()
	seedInstance(t, sourceDir)
	created, err := backup.Create(context.Background(), backup.CreateOptions{
		DataDir: sourceDir, Version: testVersion, StorageDriver: testStorageDriver,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	targetDir := t.TempDir()
	seedInstance(t, targetDir) // targetDir already has a db + uploads.

	existingDBPath := filepath.Join(targetDir, store.DBFileName)
	existingDBBefore, err := os.ReadFile(existingDBPath)
	if err != nil {
		t.Fatalf("read existing db error = %v", err)
	}

	_, err = backup.Restore(backup.RestoreOptions{DataDir: targetDir, ArchivePath: created.Path})
	if !errors.Is(err, backup.ErrExistingData) {
		t.Fatalf("Restore() without --force error = %v, want ErrExistingData", err)
	}

	existingDBAfter, err := os.ReadFile(existingDBPath)
	if err != nil {
		t.Fatalf("read existing db after refused restore error = %v", err)
	}
	if string(existingDBAfter) != string(existingDBBefore) {
		t.Fatal("Restore() without --force modified the existing database")
	}
}

// TestRestoreForceMovesExistingDataAside confirms --force preserves the
// current database (and its -wal/-shm sidecars, if present) and uploads
// directory under pre-restore-<timestamp>/ before restoring the archive
// over them.
func TestRestoreForceMovesExistingDataAside(t *testing.T) {
	sourceDir := t.TempDir()
	seed := seedInstance(t, sourceDir)
	created, err := backup.Create(context.Background(), backup.CreateOptions{
		DataDir: sourceDir, Version: testVersion, StorageDriver: testStorageDriver,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	targetDir := t.TempDir()
	targetSeed := seedInstance(t, targetDir)

	// Simulate WAL sidecar files being present alongside the db, since a
	// real running instance would have them.
	walPath := filepath.Join(targetDir, store.DBFileName+"-wal")
	if err := os.WriteFile(walPath, []byte("fake-wal"), 0o644); err != nil {
		t.Fatalf("write fake wal file error = %v", err)
	}

	result, err := backup.Restore(backup.RestoreOptions{DataDir: targetDir, ArchivePath: created.Path, Force: true})
	if err != nil {
		t.Fatalf("Restore() with --force error = %v", err)
	}
	if result.PreRestoreDir == "" {
		t.Fatal("Restore() with --force onto existing data left PreRestoreDir empty")
	}

	// The pre-restore directory holds the OLD (target) instance's db and
	// uploads, byte for byte.
	preDBPath := filepath.Join(result.PreRestoreDir, store.DBFileName)
	preDB, err := sql.Open("sqlite", preDBPath)
	if err != nil {
		t.Fatalf("sql.Open(preserved db) error = %v", err)
	}
	defer preDB.Close()
	preservedUser, err := store.GetUserByID(context.Background(), preDB, targetSeed.userID)
	if err != nil {
		t.Fatalf("GetUserByID() on preserved db error = %v", err)
	}
	if preservedUser.Email != targetSeed.userEmail {
		t.Errorf("preserved db user email = %q, want %q", preservedUser.Email, targetSeed.userEmail)
	}

	if _, err := os.Stat(filepath.Join(result.PreRestoreDir, store.DBFileName+"-wal")); err != nil {
		t.Errorf("preserved -wal sidecar missing: %v", err)
	}
	preUploadPath := filepath.Join(result.PreRestoreDir, backup.UploadsDirName, targetSeed.uploadRelKey)
	if _, err := os.Stat(preUploadPath); err != nil {
		t.Errorf("preserved uploads file missing: %v", err)
	}

	// The live data directory now holds the RESTORED (source) instance.
	liveDBPath := filepath.Join(targetDir, store.DBFileName)
	liveDB, err := sql.Open("sqlite", liveDBPath)
	if err != nil {
		t.Fatalf("sql.Open(restored db) error = %v", err)
	}
	defer liveDB.Close()
	restoredUser, err := store.GetUserByID(context.Background(), liveDB, seed.userID)
	if err != nil {
		t.Fatalf("GetUserByID() on restored (live) db error = %v", err)
	}
	if restoredUser.Email != seed.userEmail {
		t.Errorf("restored (live) db user email = %q, want %q", restoredUser.Email, seed.userEmail)
	}
}

// writeRawZip hand-builds a zip file at path from the given entries,
// bypassing every safety check backup.Create applies — the point is to
// forge archives that a real backup would never produce, so Restore's own
// validation is what has to catch them.
func writeRawZip(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %q error = %v", path, err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %q error = %v", name, err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatalf("write zip entry %q error = %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer error = %v", err)
	}
}

const validManifestJSON = `{"pocketpress_version":"0.1.0-test","created_at":"2026-08-02T00:00:00Z","storage_driver":"local"}`

// TestRestoreRejectsZipSlip hand-crafts a malicious archive containing an
// entry named "../evil" alongside an otherwise-valid manifest and database,
// and confirms Restore rejects the whole archive before writing anything —
// in particular, that no "evil" file is created outside the target data
// directory.
func TestRestoreRejectsZipSlip(t *testing.T) {
	maliciousZip := filepath.Join(t.TempDir(), "malicious.zip")
	writeRawZip(t, maliciousZip, map[string][]byte{
		backup.ManifestEntry: []byte(validManifestJSON),
		backup.DBEntry:       []byte("not a real database, but validation should never get this far"),
		"../evil":            []byte("if you can read this outside the data dir, zip-slip protection failed"),
	})

	targetDir := filepath.Join(t.TempDir(), "target")
	_, err := backup.Restore(backup.RestoreOptions{DataDir: targetDir, ArchivePath: maliciousZip})
	if !errors.Is(err, backup.ErrInvalidArchive) {
		t.Fatalf("Restore() with a zip-slip entry error = %v, want ErrInvalidArchive", err)
	}

	// Nothing should have been written at all: not the target dir, and
	// certainly not a sibling "evil" file one level above it.
	if _, statErr := os.Stat(targetDir); !os.IsNotExist(statErr) {
		t.Errorf("Restore() rejected the archive but still created %q", targetDir)
	}
	evilPath := filepath.Join(filepath.Dir(targetDir), "evil")
	if _, statErr := os.Stat(evilPath); !os.IsNotExist(statErr) {
		t.Errorf("zip-slip entry escaped: found unexpected file at %q", evilPath)
	}
}

// TestRestoreRejectsUploadsZipSlip covers the same protection for an entry
// that carries the recognized "uploads/" prefix but still climbs out of the
// uploads root once cleaned.
func TestRestoreRejectsUploadsZipSlip(t *testing.T) {
	maliciousZip := filepath.Join(t.TempDir(), "malicious-uploads.zip")
	writeRawZip(t, maliciousZip, map[string][]byte{
		backup.ManifestEntry:                    []byte(validManifestJSON),
		backup.DBEntry:                          []byte("not a real database, but validation should never get this far"),
		backup.UploadsPrefix + "../../evil.txt": []byte("escaped via the uploads prefix"),
	})

	targetDir := filepath.Join(t.TempDir(), "target")
	_, err := backup.Restore(backup.RestoreOptions{DataDir: targetDir, ArchivePath: maliciousZip})
	if !errors.Is(err, backup.ErrInvalidArchive) {
		t.Fatalf("Restore() with an uploads/-prefixed zip-slip entry error = %v, want ErrInvalidArchive", err)
	}
	if _, statErr := os.Stat(targetDir); !os.IsNotExist(statErr) {
		t.Errorf("Restore() rejected the archive but still created %q", targetDir)
	}
}

// TestRestoreRejectsMissingManifestOrDB confirms Restore validates archive
// structure before touching disk: an archive missing manifest.json, or one
// missing pocketpress.db, is rejected with ErrInvalidArchive.
func TestRestoreRejectsMissingManifestOrDB(t *testing.T) {
	tests := []struct {
		name    string
		entries map[string][]byte
	}{
		{
			name: "missing manifest",
			entries: map[string][]byte{
				backup.DBEntry: []byte("db bytes"),
			},
		},
		{
			name: "missing db",
			entries: map[string][]byte{
				backup.ManifestEntry: []byte(validManifestJSON),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "incomplete.zip")
			writeRawZip(t, archivePath, tt.entries)

			targetDir := filepath.Join(t.TempDir(), "target")
			_, err := backup.Restore(backup.RestoreOptions{DataDir: targetDir, ArchivePath: archivePath})
			if !errors.Is(err, backup.ErrInvalidArchive) {
				t.Fatalf("Restore() error = %v, want ErrInvalidArchive", err)
			}
			if _, statErr := os.Stat(targetDir); !os.IsNotExist(statErr) {
				t.Errorf("Restore() rejected the archive but still created %q", targetDir)
			}
		})
	}
}
