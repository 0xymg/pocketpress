package main

import (
	"context"
	"strings"
	"testing"

	"github.com/pocketpress/pocketpress/internal/auth"
	"github.com/pocketpress/pocketpress/internal/store"
)

// TestUserCreateCLI covers the happy path end to end through the CLI: it
// runs `user create` (which must implicitly migrate a fresh, unmigrated
// data directory, matching `migrate up`), then opens the store directly and
// confirms the stored password hash actually verifies via the same
// internal/auth code the admin API uses.
func TestUserCreateCLI(t *testing.T) {
	dataDir := t.TempDir()

	stdout, _, err := runCLI(t, dataDir, []string{
		"user", "create",
		"--email=owner@example.com",
		"--name=Owner",
		"--password=a-strong-password",
		"--role=owner",
	}, nil)
	if err != nil {
		t.Fatalf("user create error = %v", err)
	}
	if !strings.Contains(stdout, "Created user Owner <owner@example.com>") {
		t.Errorf("stdout = %q, want it to mention the created user", stdout)
	}

	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer st.Close()

	u, err := store.GetUserByEmail(context.Background(), st.DB(), "owner@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail() error = %v", err)
	}
	if u.Role != store.RoleOwner {
		t.Errorf("role = %q, want %q", u.Role, store.RoleOwner)
	}

	ok, err := auth.VerifyPassword("a-strong-password", u.PasswordHash)
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if !ok {
		t.Error("VerifyPassword() = false, want true for the password just set")
	}
}

// TestUserCreateDuplicateEmailCLI confirms the CLI surfaces the same
// duplicate-email condition the admin API rejects with 400
// "validation_failed", as a clear, non-zero-exit CLI error.
func TestUserCreateDuplicateEmailCLI(t *testing.T) {
	dataDir := t.TempDir()

	createArgs := []string{"user", "create", "--email=dup@example.com", "--name=First", "--password=a-strong-password", "--role=editor"}
	if _, _, err := runCLI(t, dataDir, createArgs, nil); err != nil {
		t.Fatalf("first user create error = %v", err)
	}

	_, _, err := runCLI(t, dataDir, []string{
		"user", "create", "--email=dup@example.com", "--name=Second", "--password=another-strong-pw", "--role=editor",
	}, nil)
	if err == nil {
		t.Fatal("second user create with a duplicate email succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want it to mention the email already exists", err.Error())
	}
}

// TestUserCreateValidationCLI exercises the same validation rules as the
// admin API's POST /users handler: email format, minimum password length,
// and the owner/editor role enum.
func TestUserCreateValidationCLI(t *testing.T) {
	base := []string{"--email=valid@example.com", "--name=Valid Name", "--password=a-strong-password", "--role=owner"}

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"missing name", replaceFlag(base, "--name=Valid Name", ""), "--name is required"},
		{"invalid email", replaceFlag(base, "--email=valid@example.com", "--email=not-an-email"), "valid email address"},
		{"short password", replaceFlag(base, "--password=a-strong-password", "--password=short"), "at least 10 characters"},
		{"invalid role", replaceFlag(base, "--role=owner", "--role=superadmin"), `"owner" or "editor"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir := t.TempDir()
			args := append([]string{"user", "create"}, tt.args...)
			_, _, err := runCLI(t, dataDir, args, nil)
			if err == nil {
				t.Fatalf("user create with %v succeeded, want a validation error", tt.args)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// replaceFlag returns a copy of args with the exact element old replaced by
// new (or, if new is "", dropped entirely) — a small helper so the
// validation table above can start from one valid flag set and mutate one
// flag at a time.
func replaceFlag(args []string, old, new string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == old {
			if new != "" {
				out = append(out, new)
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

// TestUserResetPasswordCLI covers resetting a password via --password: the
// new password verifies afterwards, and — just as importantly — the old
// password no longer does, proving the old hash was actually replaced
// rather than merely supplemented.
func TestUserResetPasswordCLI(t *testing.T) {
	dataDir := t.TempDir()
	if _, _, err := runCLI(t, dataDir, []string{
		"user", "create", "--email=reset-me@example.com", "--name=Reset Me", "--password=first-password-here", "--role=owner",
	}, nil); err != nil {
		t.Fatalf("user create error = %v", err)
	}

	stdout, _, err := runCLI(t, dataDir, []string{
		"user", "reset-password", "--email=reset-me@example.com", "--password=second-password-here",
	}, nil)
	if err != nil {
		t.Fatalf("user reset-password error = %v", err)
	}
	if !strings.Contains(stdout, "Password reset for Reset Me <reset-me@example.com>") {
		t.Errorf("stdout = %q, want it to confirm the reset", stdout)
	}

	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer st.Close()

	u, err := store.GetUserByEmail(context.Background(), st.DB(), "reset-me@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail() error = %v", err)
	}

	okNew, err := auth.VerifyPassword("second-password-here", u.PasswordHash)
	if err != nil {
		t.Fatalf("VerifyPassword(new) error = %v", err)
	}
	if !okNew {
		t.Error("new password does not verify against the stored hash")
	}

	okOld, err := auth.VerifyPassword("first-password-here", u.PasswordHash)
	if err != nil {
		t.Fatalf("VerifyPassword(old) error = %v", err)
	}
	if okOld {
		t.Error("old password still verifies against the stored hash, want it invalidated")
	}
}

// TestUserResetPasswordFromStdinCLI covers the --password-omitted path,
// which reads a single line from stdin instead.
func TestUserResetPasswordFromStdinCLI(t *testing.T) {
	dataDir := t.TempDir()
	if _, _, err := runCLI(t, dataDir, []string{
		"user", "create", "--email=stdin-user@example.com", "--name=Stdin User", "--password=original-password", "--role=editor",
	}, nil); err != nil {
		t.Fatalf("user create error = %v", err)
	}

	stdin := strings.NewReader("from-stdin-password\n")
	if _, _, err := runCLI(t, dataDir, []string{"user", "reset-password", "--email=stdin-user@example.com"}, stdin); err != nil {
		t.Fatalf("user reset-password (stdin) error = %v", err)
	}

	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer st.Close()

	u, err := store.GetUserByEmail(context.Background(), st.DB(), "stdin-user@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail() error = %v", err)
	}
	ok, err := auth.VerifyPassword("from-stdin-password", u.PasswordHash)
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if !ok {
		t.Error("password read from stdin does not verify against the stored hash")
	}
}

// TestUserResetPasswordUnknownEmailCLI confirms an unknown email produces a
// clear, non-zero-exit error rather than a generic failure or silent
// success.
func TestUserResetPasswordUnknownEmailCLI(t *testing.T) {
	dataDir := t.TempDir()
	migrateFreshStore(t, dataDir)

	_, _, err := runCLI(t, dataDir, []string{
		"user", "reset-password", "--email=nobody@example.com", "--password=whatever-password",
	}, nil)
	if err == nil {
		t.Fatal("reset-password for an unknown email succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "no user found") {
		t.Errorf("error = %q, want it to say no user was found", err.Error())
	}
}
