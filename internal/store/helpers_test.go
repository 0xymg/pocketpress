package store_test

import (
	"context"
	"testing"

	"github.com/pocketpress/pocketpress/internal/store"
)

// openTestStore opens a fresh SQLite store inside a t.TempDir() and
// migrates it to the latest schema, closing it automatically via
// t.Cleanup. Most repository tests want a fully migrated database and
// don't care about migration mechanics themselves (see migrate_test.go
// for those).
func openTestStore(t *testing.T) *store.Store {
	t.Helper()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("store.Close() error = %v", err)
		}
	})

	if _, err := store.Up(context.Background(), st.DB()); err != nil {
		t.Fatalf("store.Up() error = %v", err)
	}

	return st
}

// createTestUser inserts a single owner user for tests that need a valid
// users.id to satisfy a foreign key (sessions, post authorship, ...).
func createTestUser(t *testing.T, st *store.Store) store.User {
	t.Helper()

	u, err := store.CreateUser(context.Background(), st.DB(), store.NewUser{
		Email:        "user@example.com",
		PasswordHash: "hash",
		Name:         "Test User",
		Role:         store.RoleOwner,
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	return u
}
