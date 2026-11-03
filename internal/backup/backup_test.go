package backup_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketpress/pocketpress/internal/store"
)

// testVersion and testStorageDriver are the manifest fields used throughout
// this package's tests.
const (
	testVersion       = "0.1.0-test"
	testStorageDriver = "local"
)

// seededInstance is a freshly migrated PocketPress data directory seeded
// with one user, one post and one uploaded file, plus the IDs needed to
// re-query those rows after a round trip.
type seededInstance struct {
	dataDir string
	userID  string
	postID  string

	userEmail    string
	postSlug     string
	uploadRelKey string
	uploadBytes  []byte
}

// seedInstance opens (creating if necessary) a migrated store at dataDir,
// inserts one user and one post directly via the store package, and writes
// one file under <dataDir>/uploads — the minimal fixture the create/restore
// round-trip test needs to prove posts, users and local media all survive a
// backup and restore.
func seedInstance(t *testing.T, dataDir string) seededInstance {
	t.Helper()
	ctx := context.Background()

	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open(%q) error = %v", dataDir, err)
	}
	defer st.Close()

	if _, err := store.Up(ctx, st.DB()); err != nil {
		t.Fatalf("store.Up() error = %v", err)
	}

	user, err := store.CreateUser(ctx, st.DB(), store.NewUser{
		Email:        "seed-owner@example.com",
		PasswordHash: "argon2id$fake-hash-for-fixture-purposes",
		Name:         "Seed Owner",
		Role:         store.RoleOwner,
	})
	if err != nil {
		t.Fatalf("store.CreateUser() error = %v", err)
	}

	post, err := store.CreatePost(ctx, st.DB(), store.NewPost{
		Title:       "Backed-up post",
		Slug:        "backed-up-post",
		ContentHTML: "<p>hello from the fixture</p>",
		Status:      store.PostStatusDraft,
		Author:      user.ID,
	})
	if err != nil {
		t.Fatalf("store.CreatePost() error = %v", err)
	}

	uploadsDir := filepath.Join(dataDir, "uploads", "2026", "08")
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(uploads) error = %v", err)
	}
	uploadBytes := []byte("pretend this is a JPEG's bytes: \x00\x01\x02fixture-payload")
	uploadPath := filepath.Join(uploadsDir, "fixture.jpg")
	if err := os.WriteFile(uploadPath, uploadBytes, 0o644); err != nil {
		t.Fatalf("WriteFile(upload) error = %v", err)
	}

	return seededInstance{
		dataDir:      dataDir,
		userID:       user.ID,
		postID:       post.ID,
		userEmail:    user.Email,
		postSlug:     post.Slug,
		uploadRelKey: filepath.Join("2026", "08", "fixture.jpg"),
		uploadBytes:  uploadBytes,
	}
}
