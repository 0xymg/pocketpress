package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

// TestSessionsCreateGetValidExpireDelete exercises the sessions
// repository's core lifecycle: create, a valid lookup, an expired lookup,
// an inactive-user lookup, and delete-by-token-hash (logout).
func TestSessionsCreateGetValidExpireDelete(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	user := createTestUser(t, st)

	now := time.Now().UTC()
	session, err := store.CreateSession(ctx, st.DB(), store.NewSession{
		UserID:    user.ID,
		TokenHash: "token-hash",
		ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if session.ID == "" {
		t.Fatal("CreateSession() returned an empty ID")
	}

	valid, err := store.GetValidSessionByTokenHash(ctx, st.DB(), "token-hash", now)
	if err != nil {
		t.Fatalf("GetValidSessionByTokenHash() error = %v", err)
	}
	if valid.ID != session.ID {
		t.Fatalf("GetValidSessionByTokenHash().ID = %q, want %q", valid.ID, session.ID)
	}

	// Expired as of a time after expires_at.
	_, err = store.GetValidSessionByTokenHash(ctx, st.DB(), "token-hash", now.Add(2*time.Hour))
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetValidSessionByTokenHash() (expired) error = %v, want ErrNotFound", err)
	}

	// Session unexpired but the owning user is inactive.
	if err := store.SetUserActive(ctx, st.DB(), user.ID, false); err != nil {
		t.Fatalf("SetUserActive(false) error = %v", err)
	}
	_, err = store.GetValidSessionByTokenHash(ctx, st.DB(), "token-hash", now)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetValidSessionByTokenHash() (inactive user) error = %v, want ErrNotFound", err)
	}
	if err := store.SetUserActive(ctx, st.DB(), user.ID, true); err != nil {
		t.Fatalf("SetUserActive(true) error = %v", err)
	}

	if err := store.DeleteSessionByTokenHash(ctx, st.DB(), "token-hash"); err != nil {
		t.Fatalf("DeleteSessionByTokenHash() error = %v", err)
	}
	_, err = store.GetValidSessionByTokenHash(ctx, st.DB(), "token-hash", now)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetValidSessionByTokenHash() (deleted) error = %v, want ErrNotFound", err)
	}

	// Deleting an already-deleted token hash is a no-op, not an error
	// (idempotent logout).
	if err := store.DeleteSessionByTokenHash(ctx, st.DB(), "token-hash"); err != nil {
		t.Fatalf("DeleteSessionByTokenHash() (already deleted) error = %v", err)
	}
}

// TestDeleteExpiredSessions confirms only expired sessions are purged.
func TestDeleteExpiredSessions(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	user := createTestUser(t, st)
	now := time.Now().UTC()

	if _, err := store.CreateSession(ctx, st.DB(), store.NewSession{
		UserID: user.ID, TokenHash: "expired", ExpiresAt: now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("CreateSession() (expired) error = %v", err)
	}
	if _, err := store.CreateSession(ctx, st.DB(), store.NewSession{
		UserID: user.ID, TokenHash: "still-valid", ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateSession() (valid) error = %v", err)
	}

	n, err := store.DeleteExpiredSessions(ctx, st.DB(), now)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("DeleteExpiredSessions() = %d, want 1", n)
	}

	if _, err := store.GetValidSessionByTokenHash(ctx, st.DB(), "still-valid", now); err != nil {
		t.Fatalf("expected the still-valid session to remain: %v", err)
	}
}

// TestSessionsDeletedOnUserDeleteCascade confirms sessions.user_id's
// ON DELETE CASCADE actually fires under the foreign_keys=ON pragma.
func TestSessionsDeletedOnUserDeleteCascade(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	user := createTestUser(t, st)
	now := time.Now().UTC()

	if _, err := store.CreateSession(ctx, st.DB(), store.NewSession{
		UserID: user.ID, TokenHash: "cascade-me", ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if _, err := st.DB().ExecContext(ctx, `DELETE FROM users WHERE id = ?`, user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var count int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, user.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected sessions to cascade-delete with their user, found %d remaining", count)
	}
}

// TestDeleteSessionsByUserID confirms it removes every session for a user
// without requiring each token hash individually (e.g. "log out
// everywhere").
func TestDeleteSessionsByUserID(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	user := createTestUser(t, st)
	now := time.Now().UTC()

	for _, tokenHash := range []string{"a", "b"} {
		if _, err := store.CreateSession(ctx, st.DB(), store.NewSession{
			UserID: user.ID, TokenHash: tokenHash, ExpiresAt: now.Add(time.Hour),
		}); err != nil {
			t.Fatalf("CreateSession() error = %v", err)
		}
	}

	if err := store.DeleteSessionsByUserID(ctx, st.DB(), user.ID); err != nil {
		t.Fatalf("DeleteSessionsByUserID() error = %v", err)
	}

	var count int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, user.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 sessions after DeleteSessionsByUserID, found %d", count)
	}
}
