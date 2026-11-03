package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

// TestUsersCRUD exercises the users repository end to end: create, both
// lookups, last-login and active updates, listing, and role counting.
func TestUsersCRUD(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	created, err := store.CreateUser(ctx, st.DB(), store.NewUser{
		Email:        "Owner@Example.com",
		PasswordHash: "hash",
		Name:         "Owner",
		Role:         store.RoleOwner,
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if created.ID == "" {
		t.Fatal("CreateUser() returned an empty ID")
	}
	if !created.Active {
		t.Fatal("CreateUser() returned Active = false, want true")
	}

	byID, err := store.GetUserByID(ctx, st.DB(), created.ID)
	if err != nil {
		t.Fatalf("GetUserByID() error = %v", err)
	}
	if byID.Email != created.Email {
		t.Fatalf("GetUserByID().Email = %q, want %q", byID.Email, created.Email)
	}

	// GetUserByEmail must be case-insensitive (users.email is COLLATE
	// NOCASE).
	byEmail, err := store.GetUserByEmail(ctx, st.DB(), "owner@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail() (lowercased) error = %v", err)
	}
	if byEmail.ID != created.ID {
		t.Fatalf("GetUserByEmail() ID = %q, want %q", byEmail.ID, created.ID)
	}

	loginAt := time.Now().UTC().Truncate(time.Second)
	if err := store.UpdateUserLastLogin(ctx, st.DB(), created.ID, loginAt); err != nil {
		t.Fatalf("UpdateUserLastLogin() error = %v", err)
	}
	afterLogin, err := store.GetUserByID(ctx, st.DB(), created.ID)
	if err != nil {
		t.Fatalf("GetUserByID() after login error = %v", err)
	}
	if afterLogin.LastLoginAt == nil || !afterLogin.LastLoginAt.Equal(loginAt) {
		t.Fatalf("LastLoginAt = %v, want %v", afterLogin.LastLoginAt, loginAt)
	}

	if err := store.SetUserActive(ctx, st.DB(), created.ID, false); err != nil {
		t.Fatalf("SetUserActive(false) error = %v", err)
	}
	deactivated, err := store.GetUserByID(ctx, st.DB(), created.ID)
	if err != nil {
		t.Fatalf("GetUserByID() after deactivate error = %v", err)
	}
	if deactivated.Active {
		t.Fatal("Active = true after SetUserActive(false)")
	}

	if _, err := store.CreateUser(ctx, st.DB(), store.NewUser{
		Email:        "editor@example.com",
		PasswordHash: "hash",
		Name:         "Editor",
		Role:         store.RoleEditor,
	}); err != nil {
		t.Fatalf("CreateUser() (editor) error = %v", err)
	}

	users, err := store.ListUsers(ctx, st.DB())
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("ListUsers() returned %d users, want 2", len(users))
	}
	if users[0].ID != created.ID {
		t.Fatalf("ListUsers()[0].ID = %q, want %q (oldest first)", users[0].ID, created.ID)
	}

	ownerCount, err := store.CountUsersByRole(ctx, st.DB(), store.RoleOwner)
	if err != nil {
		t.Fatalf("CountUsersByRole(owner) error = %v", err)
	}
	if ownerCount != 1 {
		t.Fatalf("CountUsersByRole(owner) = %d, want 1", ownerCount)
	}

	editorCount, err := store.CountUsersByRole(ctx, st.DB(), store.RoleEditor)
	if err != nil {
		t.Fatalf("CountUsersByRole(editor) error = %v", err)
	}
	if editorCount != 1 {
		t.Fatalf("CountUsersByRole(editor) = %d, want 1", editorCount)
	}
}

// TestGetUserByIDNotFound confirms unknown IDs return the ErrNotFound
// sentinel rather than a raw sql.ErrNoRows or a zero-value user.
func TestGetUserByIDNotFound(t *testing.T) {
	st := openTestStore(t)

	_, err := store.GetUserByID(context.Background(), st.DB(), "does-not-exist")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetUserByID() error = %v, want ErrNotFound", err)
	}
}

// TestGetUserByEmailNotFound mirrors TestGetUserByIDNotFound for the
// email lookup path.
func TestGetUserByEmailNotFound(t *testing.T) {
	st := openTestStore(t)

	_, err := store.GetUserByEmail(context.Background(), st.DB(), "nobody@example.com")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetUserByEmail() error = %v, want ErrNotFound", err)
	}
}

// TestCreateUserDuplicateEmailCaseInsensitiveFails confirms the
// users.email COLLATE NOCASE UNIQUE constraint rejects a second user
// whose email differs only by case.
func TestCreateUserDuplicateEmailCaseInsensitiveFails(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	if _, err := store.CreateUser(ctx, st.DB(), store.NewUser{
		Email:        "dup@example.com",
		PasswordHash: "hash",
		Name:         "First",
		Role:         store.RoleOwner,
	}); err != nil {
		t.Fatalf("CreateUser() (first) error = %v", err)
	}

	_, err := store.CreateUser(ctx, st.DB(), store.NewUser{
		Email:        "DUP@Example.com",
		PasswordHash: "hash",
		Name:         "Second",
		Role:         store.RoleEditor,
	})
	if err == nil {
		t.Fatal("CreateUser() (case-insensitive duplicate email) error = nil, want a constraint error")
	}
}

// TestSetUserActiveNotFound confirms updating an unknown user returns
// ErrNotFound instead of silently succeeding.
func TestSetUserActiveNotFound(t *testing.T) {
	st := openTestStore(t)

	err := store.SetUserActive(context.Background(), st.DB(), "does-not-exist", false)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("SetUserActive() error = %v, want ErrNotFound", err)
	}
}

// TestSetUserFieldMutators exercises the name, role and password-hash
// setters, including their ErrNotFound behavior on an unknown id.
func TestSetUserFieldMutators(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	u := createTestUser(t, st)

	if err := store.SetUserName(ctx, st.DB(), u.ID, "Renamed"); err != nil {
		t.Fatalf("SetUserName() error = %v", err)
	}
	if err := store.SetUserRole(ctx, st.DB(), u.ID, store.RoleEditor); err != nil {
		t.Fatalf("SetUserRole() error = %v", err)
	}
	if err := store.SetUserPasswordHash(ctx, st.DB(), u.ID, "new-hash"); err != nil {
		t.Fatalf("SetUserPasswordHash() error = %v", err)
	}

	got, err := store.GetUserByID(ctx, st.DB(), u.ID)
	if err != nil {
		t.Fatalf("GetUserByID() error = %v", err)
	}
	if got.Name != "Renamed" || got.Role != store.RoleEditor || got.PasswordHash != "new-hash" {
		t.Fatalf("after mutators = {name:%q role:%q hash:%q}, want {Renamed editor new-hash}", got.Name, got.Role, got.PasswordHash)
	}

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"SetUserName", func() error { return store.SetUserName(ctx, st.DB(), "missing", "x") }},
		{"SetUserRole", func() error { return store.SetUserRole(ctx, st.DB(), "missing", store.RoleEditor) }},
		{"SetUserPasswordHash", func() error { return store.SetUserPasswordHash(ctx, st.DB(), "missing", "x") }},
	} {
		if err := tc.call(); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("%s(unknown id) error = %v, want ErrNotFound", tc.name, err)
		}
	}
}
