package auth

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

// newTestService opens a fresh, migrated SQLite store in a t.TempDir() and
// returns a Service over it, closing the store via t.Cleanup.
func newTestService(t *testing.T) *Service {
	t.Helper()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if _, err := store.Up(context.Background(), st.DB()); err != nil {
		t.Fatalf("store.Up() error = %v", err)
	}

	return NewService(st.DB(), slog.New(slog.DiscardHandler))
}

func validSetup() SetupInput {
	return SetupInput{
		SiteName:   "My Blog",
		SiteURL:    "https://example.com",
		OwnerName:  "Owner",
		OwnerEmail: "owner@example.com",
		Password:   "a-strong-password",
		Timezone:   "UTC",
	}
}

// TestSetupCreatesOwnerAndSession verifies a fresh setup creates the owner,
// flips IsInitialized to true, and returns a working session token.
func TestSetupCreatesOwnerAndSession(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	if initialized, err := svc.IsInitialized(ctx); err != nil || initialized {
		t.Fatalf("IsInitialized() before setup = (%v, %v), want (false, nil)", initialized, err)
	}

	owner, token, err := svc.Setup(ctx, validSetup())
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if owner.Role != store.RoleOwner {
		t.Fatalf("owner role = %q, want %q", owner.Role, store.RoleOwner)
	}
	if token == "" {
		t.Fatal("Setup() returned an empty session token")
	}

	if initialized, err := svc.IsInitialized(ctx); err != nil || !initialized {
		t.Fatalf("IsInitialized() after setup = (%v, %v), want (true, nil)", initialized, err)
	}

	user, err := svc.UserForSessionToken(ctx, token)
	if err != nil {
		t.Fatalf("UserForSessionToken() error = %v", err)
	}
	if user.ID != owner.ID {
		t.Fatalf("session resolved to user %q, want %q", user.ID, owner.ID)
	}
}

// TestSetupTwiceFails confirms setup is permanently disabled after the
// first run.
func TestSetupTwiceFails(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	if _, _, err := svc.Setup(ctx, validSetup()); err != nil {
		t.Fatalf("Setup() (first) error = %v", err)
	}

	_, _, err := svc.Setup(ctx, validSetup())
	if !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("Setup() (second) error = %v, want ErrAlreadyInitialized", err)
	}
}

// TestLoginResultsAreIndistinguishable confirms that unknown email, wrong
// password, and an inactive user all return ErrInvalidCredentials.
func TestLoginResultsAreIndistinguishable(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	if _, _, err := svc.Setup(ctx, validSetup()); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	// Correct login works first.
	if _, _, err := svc.Login(ctx, "owner@example.com", "a-strong-password"); err != nil {
		t.Fatalf("Login() (correct) error = %v", err)
	}

	cases := []struct {
		name, email, password string
	}{
		{"unknown email", "nobody@example.com", "a-strong-password"},
		{"wrong password", "owner@example.com", "wrong-password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := svc.Login(ctx, tc.email, tc.password); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
			}
		})
	}

	// Deactivate the owner via a second owner (so the last-owner guard does
	// not block it), then confirm the deactivated owner cannot log in.
	editor, err := svc.CreateUser(ctx, NewUserInput{Name: "Two", Email: "two@example.com", Password: "another-strong-pass", Role: store.RoleOwner})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	_ = editor
	inactive := false
	owner, _ := store.GetUserByEmail(ctx, dbOf(t, svc), "owner@example.com")
	if _, err := svc.UpdateUser(ctx, owner.ID, UpdateUserInput{Active: &inactive}); err != nil {
		t.Fatalf("UpdateUser(deactivate) error = %v", err)
	}
	if _, _, err := svc.Login(ctx, "owner@example.com", "a-strong-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() (inactive) error = %v, want ErrInvalidCredentials", err)
	}
}

// TestLogoutIsIdempotent confirms logout succeeds with an empty or unknown
// token.
func TestLogoutIsIdempotent(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	if err := svc.Logout(ctx, ""); err != nil {
		t.Fatalf("Logout(empty) error = %v", err)
	}
	if err := svc.Logout(ctx, "not-a-real-token"); err != nil {
		t.Fatalf("Logout(unknown) error = %v", err)
	}
}

// TestUserForSessionTokenRejectsExpired confirms an expired session does
// not resolve to a user.
func TestUserForSessionTokenRejectsExpired(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	owner, _, err := svc.Setup(ctx, validSetup())
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	// Insert a session that already expired, directly via the store.
	token, err := newSessionToken()
	if err != nil {
		t.Fatalf("newSessionToken() error = %v", err)
	}
	if _, err := store.CreateSession(ctx, dbOf(t, svc), store.NewSession{
		UserID:    owner.ID,
		TokenHash: hashToken(token),
		ExpiresAt: time.Now().UTC().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if _, err := svc.UserForSessionToken(ctx, token); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("UserForSessionToken(expired) error = %v, want ErrNotFound", err)
	}
}

// TestCreateUserRejectsDuplicateEmail confirms the ErrEmailTaken path.
func TestCreateUserRejectsDuplicateEmail(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	if _, _, err := svc.Setup(ctx, validSetup()); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	_, err := svc.CreateUser(ctx, NewUserInput{Name: "Dup", Email: "OWNER@example.com", Password: "another-strong-pass", Role: store.RoleEditor})
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("CreateUser(duplicate) error = %v, want ErrEmailTaken", err)
	}
}

// TestUpdateUserLastOwnerGuard confirms the last active owner cannot be
// deactivated or demoted, but that the guard lifts once a second owner
// exists.
func TestUpdateUserLastOwnerGuard(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	owner, _, err := svc.Setup(ctx, validSetup())
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	inactive := false
	if _, err := svc.UpdateUser(ctx, owner.ID, UpdateUserInput{Active: &inactive}); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("UpdateUser(deactivate last owner) error = %v, want ErrLastOwner", err)
	}
	editorRole := store.RoleEditor
	if _, err := svc.UpdateUser(ctx, owner.ID, UpdateUserInput{Role: &editorRole}); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("UpdateUser(demote last owner) error = %v, want ErrLastOwner", err)
	}

	// Add a second owner; now the original can be demoted.
	second, err := svc.CreateUser(ctx, NewUserInput{Name: "Two", Email: "two@example.com", Password: "another-strong-pass", Role: store.RoleOwner})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	_ = second
	updated, err := svc.UpdateUser(ctx, owner.ID, UpdateUserInput{Role: &editorRole})
	if err != nil {
		t.Fatalf("UpdateUser(demote with two owners) error = %v", err)
	}
	if updated.Role != store.RoleEditor {
		t.Fatalf("role after demote = %q, want %q", updated.Role, store.RoleEditor)
	}
}

// TestUpdateUserAppliesFields confirms name and password updates take
// effect (the new password verifies; the old one no longer does).
func TestUpdateUserAppliesFields(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	owner, _, err := svc.Setup(ctx, validSetup())
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	newName := "Renamed Owner"
	newPass := "brand-new-password"
	updated, err := svc.UpdateUser(ctx, owner.ID, UpdateUserInput{Name: &newName, Password: &newPass})
	if err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}
	if updated.Name != newName {
		t.Fatalf("name = %q, want %q", updated.Name, newName)
	}

	if _, _, err := svc.Login(ctx, "owner@example.com", newPass); err != nil {
		t.Fatalf("Login() with new password error = %v", err)
	}
	if _, _, err := svc.Login(ctx, "owner@example.com", "a-strong-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() with old password error = %v, want ErrInvalidCredentials", err)
	}
}

// dbOf reaches into the service's store handle for tests that need to seed
// rows directly.
func dbOf(t *testing.T, s *Service) *sql.DB {
	t.Helper()
	return s.db
}
