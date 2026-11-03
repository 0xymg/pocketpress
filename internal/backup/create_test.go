package backup_test

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/pocketpress/pocketpress/internal/backup"
	"github.com/pocketpress/pocketpress/internal/store"
)

// openZip opens path and fails the test if it is not a readable zip
// archive.
func openZip(t *testing.T, path string) *zip.ReadCloser {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("zip.OpenReader(%q) error = %v", path, err)
	}
	t.Cleanup(func() { zr.Close() })
	return zr
}

// zipEntry returns the named entry from zr, or fails the test.
func zipEntry(t *testing.T, zr *zip.ReadCloser, name string) *zip.File {
	t.Helper()
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("zip archive missing entry %q", name)
	return nil
}

// extractZipEntry copies f's content to a new file under t.TempDir() and
// returns its path.
func extractZipEntry(t *testing.T, f *zip.File, destName string) string {
	t.Helper()
	rc, err := f.Open()
	if err != nil {
		t.Fatalf("open zip entry %q error = %v", f.Name, err)
	}
	defer rc.Close()

	destPath := filepath.Join(t.TempDir(), destName)
	dest, err := os.Create(destPath)
	if err != nil {
		t.Fatalf("create %q error = %v", destPath, err)
	}
	defer dest.Close()

	if _, err := io.Copy(dest, rc); err != nil {
		t.Fatalf("copy zip entry %q error = %v", f.Name, err)
	}
	return destPath
}

// TestCreateProducesValidArchive seeds a data directory, runs Create, and
// inspects the resulting zip: a manifest with the expected fields, a
// pocketpress.db entry that is itself a valid, openable SQLite database
// containing the seeded user, and the seeded upload file under uploads/.
func TestCreateProducesValidArchive(t *testing.T) {
	dataDir := t.TempDir()
	seed := seedInstance(t, dataDir)

	result, err := backup.Create(context.Background(), backup.CreateOptions{
		DataDir:       dataDir,
		Version:       testVersion,
		StorageDriver: testStorageDriver,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.Path == "" {
		t.Fatal("Create() returned an empty Path")
	}
	if result.Size <= 0 {
		t.Fatalf("Create() Size = %d, want > 0", result.Size)
	}
	if info, err := os.Stat(result.Path); err != nil || info.Size() != result.Size {
		t.Fatalf("archive at %q: stat error = %v, or size mismatch", result.Path, err)
	}

	// Default location: <dataDir>/backups/pocketpress-backup-<timestamp>.zip.
	wantDir := filepath.Join(dataDir, backup.BackupsDirName)
	if gotDir := filepath.Dir(result.Path); gotDir != wantDir {
		t.Fatalf("archive directory = %q, want %q", gotDir, wantDir)
	}
	if !strings.HasPrefix(filepath.Base(result.Path), "pocketpress-backup-") || !strings.HasSuffix(result.Path, ".zip") {
		t.Fatalf("archive name %q does not match the pocketpress-backup-<timestamp>.zip pattern", filepath.Base(result.Path))
	}

	zr := openZip(t, result.Path)

	// --- manifest.json ---
	manifestFile := zipEntry(t, zr, backup.ManifestEntry)
	rc, err := manifestFile.Open()
	if err != nil {
		t.Fatalf("open manifest entry error = %v", err)
	}
	var manifest backup.Manifest
	if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
		t.Fatalf("decode manifest error = %v", err)
	}
	rc.Close()

	if manifest.PocketPressVersion != testVersion {
		t.Errorf("manifest.PocketPressVersion = %q, want %q", manifest.PocketPressVersion, testVersion)
	}
	if manifest.StorageDriver != testStorageDriver {
		t.Errorf("manifest.StorageDriver = %q, want %q", manifest.StorageDriver, testStorageDriver)
	}
	createdAt, err := time.Parse(time.RFC3339, manifest.CreatedAt)
	if err != nil {
		t.Fatalf("manifest.CreatedAt = %q is not RFC3339: %v", manifest.CreatedAt, err)
	}
	if createdAt.Location() != time.UTC {
		// time.Parse on an RFC3339 string with a "Z" offset yields UTC; a
		// non-UTC location here would mean we serialized a local time
		// instead.
		t.Errorf("manifest.CreatedAt = %q, want a UTC (Z) timestamp", manifest.CreatedAt)
	}
	if time.Since(createdAt) < 0 || time.Since(createdAt) > time.Minute {
		t.Errorf("manifest.CreatedAt = %v is not close to now", createdAt)
	}

	// --- pocketpress.db ---
	dbFile := zipEntry(t, zr, backup.DBEntry)
	dbCopyPath := extractZipEntry(t, dbFile, "extracted.db")

	db, err := sql.Open("sqlite", dbCopyPath)
	if err != nil {
		t.Fatalf("sql.Open(extracted db) error = %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping extracted db error = %v (not a valid SQLite database)", err)
	}

	restoredUser, err := store.GetUserByID(context.Background(), db, seed.userID)
	if err != nil {
		t.Fatalf("GetUserByID() on extracted db error = %v", err)
	}
	if restoredUser.Email != seed.userEmail {
		t.Errorf("extracted db user email = %q, want %q", restoredUser.Email, seed.userEmail)
	}

	// --- uploads/ ---
	wantUploadEntry := backup.UploadsPrefix + filepath.ToSlash(seed.uploadRelKey)
	uploadFile := zipEntry(t, zr, wantUploadEntry)
	uploadCopyPath := extractZipEntry(t, uploadFile, "extracted-upload")
	gotBytes, err := os.ReadFile(uploadCopyPath)
	if err != nil {
		t.Fatalf("read extracted upload error = %v", err)
	}
	if string(gotBytes) != string(seed.uploadBytes) {
		t.Errorf("extracted upload bytes = %q, want %q", gotBytes, seed.uploadBytes)
	}
}

// TestCreateSkipsMissingUploadsDirectory confirms Create does not error and
// simply omits uploads/ entries when <dataDir>/uploads does not exist.
func TestCreateSkipsMissingUploadsDirectory(t *testing.T) {
	dataDir := t.TempDir()

	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	if _, err := store.Up(context.Background(), st.DB()); err != nil {
		t.Fatalf("store.Up() error = %v", err)
	}
	st.Close()

	// No uploads directory was ever created for this data dir.
	if _, err := os.Stat(filepath.Join(dataDir, "uploads")); !os.IsNotExist(err) {
		t.Fatalf("test setup: expected no uploads dir, stat error = %v", err)
	}

	result, err := backup.Create(context.Background(), backup.CreateOptions{
		DataDir:       dataDir,
		Version:       testVersion,
		StorageDriver: testStorageDriver,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	zr := openZip(t, result.Path)
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, backup.UploadsPrefix) {
			t.Errorf("archive unexpectedly contains upload entry %q", f.Name)
		}
	}
	// The db and manifest entries must still be present.
	zipEntry(t, zr, backup.DBEntry)
	zipEntry(t, zr, backup.ManifestEntry)
}

// TestCreateOutOptions covers the --out contract: "" (default location), a
// directory (auto-named file inside it), and a full ".zip" path.
func TestCreateOutOptions(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	if _, err := store.Up(context.Background(), st.DB()); err != nil {
		t.Fatalf("store.Up() error = %v", err)
	}
	st.Close()

	t.Run("explicit directory", func(t *testing.T) {
		outDir := t.TempDir()
		result, err := backup.Create(context.Background(), backup.CreateOptions{
			DataDir: dataDir, Out: outDir, Version: testVersion, StorageDriver: testStorageDriver,
		})
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if filepath.Dir(result.Path) != outDir {
			t.Fatalf("archive directory = %q, want %q", filepath.Dir(result.Path), outDir)
		}
	})

	t.Run("explicit zip path", func(t *testing.T) {
		outFile := filepath.Join(t.TempDir(), "my-backup.zip")
		result, err := backup.Create(context.Background(), backup.CreateOptions{
			DataDir: dataDir, Out: outFile, Version: testVersion, StorageDriver: testStorageDriver,
		})
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if result.Path != outFile {
			t.Fatalf("archive path = %q, want %q", result.Path, outFile)
		}
	})
}
