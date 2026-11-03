package media_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketpress/pocketpress/internal/media"
)

// TestLocalDriverSaveOpenDelete covers the driver's basic contract: Save
// writes bytes retrievable by Open, and Delete removes them (after which
// Open reports media.ErrNotFound).
func TestLocalDriverSaveOpenDelete(t *testing.T) {
	dir := t.TempDir()
	d, err := media.NewLocalDriver(dir)
	if err != nil {
		t.Fatalf("NewLocalDriver() error = %v", err)
	}

	ctx := context.Background()
	key := "2026/08/some-id.png"
	content := []byte("fake png bytes")

	if err := d.Save(ctx, key, bytes.NewReader(content), int64(len(content)), "image/png"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// The file actually lands under <dataDir>/uploads/<key>.
	wantPath := filepath.Join(dir, "uploads", "2026", "08", "some-id.png")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected file at %q: %v", wantPath, err)
	}

	rc, err := d.Open(ctx, key)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("read opened file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("Open() content = %q, want %q", got, content)
	}

	if err := d.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := os.Stat(wantPath); !os.IsNotExist(err) {
		t.Fatalf("expected file to be gone after Delete(), stat error = %v", err)
	}

	// Open after Delete reports ErrNotFound.
	if _, err := d.Open(ctx, key); !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("Open() after Delete() error = %v, want media.ErrNotFound", err)
	}
}

// TestLocalDriverOpenMissingKey confirms Open on a never-written key returns
// media.ErrNotFound (not a raw os.ErrNotExist or a generic error).
func TestLocalDriverOpenMissingKey(t *testing.T) {
	d, err := media.NewLocalDriver(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalDriver() error = %v", err)
	}
	if _, err := d.Open(context.Background(), "2026/08/never-written.png"); !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("Open() missing key error = %v, want media.ErrNotFound", err)
	}
}

// TestLocalDriverDeleteMissingKeyIsIdempotent confirms deleting a key that
// was never written (or already deleted) is not an error.
func TestLocalDriverDeleteMissingKeyIsIdempotent(t *testing.T) {
	d, err := media.NewLocalDriver(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalDriver() error = %v", err)
	}
	if err := d.Delete(context.Background(), "2026/08/never-written.png"); err != nil {
		t.Fatalf("Delete() missing key error = %v, want nil (idempotent)", err)
	}
}

// TestLocalDriverRejectsPathTraversal confirms every key that would resolve
// outside <dataDir>/uploads is rejected by Save/Open/Delete, instead of
// silently reading or writing outside the storage root.
func TestLocalDriverRejectsPathTraversal(t *testing.T) {
	dataDir := t.TempDir()
	d, err := media.NewLocalDriver(dataDir)
	if err != nil {
		t.Fatalf("NewLocalDriver() error = %v", err)
	}

	// A canary file just outside the uploads root, so a successful escape
	// would be independently observable (belt and suspenders alongside the
	// error checks below).
	canary := filepath.Join(dataDir, "canary.txt")
	if err := os.WriteFile(canary, []byte("do not touch"), 0o644); err != nil {
		t.Fatalf("write canary file: %v", err)
	}

	ctx := context.Background()
	traversalKeys := []string{
		"../canary.txt",
		"../../etc/passwd",
		"2026/../../canary.txt",
		"../../../../../../etc/passwd",
	}
	for _, key := range traversalKeys {
		t.Run(key, func(t *testing.T) {
			if err := d.Save(ctx, key, bytes.NewReader([]byte("x")), 1, "image/png"); err == nil {
				t.Fatalf("Save(%q) succeeded, want a traversal error", key)
			}
			if _, err := d.Open(ctx, key); err == nil {
				t.Fatalf("Open(%q) succeeded, want a traversal error", key)
			}
			if err := d.Delete(ctx, key); err == nil {
				t.Fatalf("Delete(%q) succeeded, want a traversal error", key)
			}
		})
	}

	// An absolute path outside dataDir is rejected the same way.
	absoluteKey := canary
	if err := d.Save(ctx, absoluteKey, bytes.NewReader([]byte("x")), 1, "image/png"); err == nil {
		t.Fatalf("Save(%q) (absolute path) succeeded, want a traversal error", absoluteKey)
	}

	// Empty key is rejected outright.
	if err := d.Save(ctx, "", bytes.NewReader([]byte("x")), 1, "image/png"); err == nil {
		t.Fatal("Save(\"\") succeeded, want an error")
	}

	// The canary is untouched.
	got, err := os.ReadFile(canary)
	if err != nil {
		t.Fatalf("read canary file: %v", err)
	}
	if string(got) != "do not touch" {
		t.Fatalf("canary file content = %q, want it untouched", got)
	}
}

// TestLocalDriverCreatesDataDir confirms NewLocalDriver creates
// <dataDir>/uploads (and dataDir itself) up front.
func TestLocalDriverCreatesDataDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "nested", "data")
	if _, err := media.NewLocalDriver(dataDir); err != nil {
		t.Fatalf("NewLocalDriver() error = %v", err)
	}
	info, err := os.Stat(filepath.Join(dataDir, "uploads"))
	if err != nil || !info.IsDir() {
		t.Fatalf("expected <dataDir>/uploads to exist, stat error = %v", err)
	}
}
