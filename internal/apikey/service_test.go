package apikey_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/apikey"
	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/store"
)

func openTestDB(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if _, err := store.Up(context.Background(), st.DB()); err != nil {
		t.Fatalf("store.Up: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestCreateListAuthenticateRevoke(t *testing.T) {
	st := openTestDB(t)
	svc := apikey.NewService(st.DB(), nil)
	ctx := context.Background()

	k, raw, err := svc.Create(ctx, apikey.CreateInput{
		Name:   "CI reader",
		Scopes: []string{apikey.ScopeContentRead, apikey.ScopePreviewRead, apikey.ScopeContentRead},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(raw, apikey.KeyPrefix) {
		t.Fatalf("raw = %q, want ppkey_ prefix", raw)
	}
	if k.Prefix != apikey.DisplayPrefix(raw) {
		t.Fatalf("prefix = %q, want %q", k.Prefix, apikey.DisplayPrefix(raw))
	}
	if k.TokenHash != apikey.Hash(raw) {
		t.Fatal("stored hash does not match Hash(raw)")
	}
	if len(k.Scopes) != 2 {
		t.Fatalf("scopes = %v, want deduped length 2", k.Scopes)
	}

	list, err := svc.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v, %v; want 1 key", list, err)
	}

	got, err := svc.Authenticate(ctx, raw)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.ID != k.ID || got.LastUsedAt == nil {
		t.Fatalf("Authenticate result = %+v", got)
	}

	if _, err := svc.Authenticate(ctx, "ppkey_not-a-real-key"); !errors.Is(err, apikey.ErrInvalidKey) {
		t.Fatalf("bad key err = %v, want ErrInvalidKey", err)
	}

	revoked, err := svc.Revoke(ctx, k.ID)
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if revoked.RevokedAt == nil {
		t.Fatal("Revoke left revoked_at nil")
	}
	if _, err := svc.Authenticate(ctx, raw); !errors.Is(err, apikey.ErrInvalidKey) {
		t.Fatalf("Authenticate after revoke = %v, want ErrInvalidKey", err)
	}
}

func TestCreateValidation(t *testing.T) {
	st := openTestDB(t)
	svc := apikey.NewService(st.DB(), nil)
	ctx := context.Background()

	past := time.Now().UTC().Add(-time.Hour)
	cases := []struct {
		name  string
		in    apikey.CreateInput
		field string
	}{
		{"missing name", apikey.CreateInput{Scopes: []string{apikey.ScopeContentRead}}, "name"},
		{"missing scopes", apikey.CreateInput{Name: "X"}, "scopes"},
		{"bad scope", apikey.CreateInput{Name: "X", Scopes: []string{"admin:write"}}, "scopes"},
		{"past expiry", apikey.CreateInput{Name: "X", Scopes: []string{apikey.ScopeContentRead}, ExpiresAt: &past}, "expires_at"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := svc.Create(ctx, tc.in)
			var ve *content.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("err = %v, want ValidationError", err)
			}
			if _, ok := ve.Fields[tc.field]; !ok {
				t.Fatalf("fields = %v, want %q", ve.Fields, tc.field)
			}
		})
	}
}

func TestRequireScope(t *testing.T) {
	st := openTestDB(t)
	svc := apikey.NewService(st.DB(), nil)
	ctx := context.Background()

	_, raw, err := svc.Create(ctx, apikey.CreateInput{
		Name:   "content only",
		Scopes: []string{apikey.ScopeContentRead},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RequireScope(ctx, raw, apikey.ScopeContentRead); err != nil {
		t.Fatalf("content:read: %v", err)
	}
	if _, err := svc.RequireScope(ctx, raw, apikey.ScopePreviewRead); !errors.Is(err, apikey.ErrMissingScope) {
		t.Fatalf("preview:read err = %v, want ErrMissingScope", err)
	}
}

func TestExpiredKeyRejected(t *testing.T) {
	st := openTestDB(t)
	svc := apikey.NewService(st.DB(), nil)
	ctx := context.Background()

	expires := time.Now().UTC().Add(50 * time.Millisecond)
	_, raw, err := svc.Create(ctx, apikey.CreateInput{
		Name:      "short lived",
		Scopes:    []string{apikey.ScopeContentRead},
		ExpiresAt: &expires,
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if _, err := svc.Authenticate(ctx, raw); !errors.Is(err, apikey.ErrInvalidKey) {
		t.Fatalf("Authenticate expired = %v, want ErrInvalidKey", err)
	}
}

func TestRawKeyNeverStored(t *testing.T) {
	st := openTestDB(t)
	svc := apikey.NewService(st.DB(), nil)
	ctx := context.Background()

	_, raw, err := svc.Create(ctx, apikey.CreateInput{
		Name:   "secret",
		Scopes: []string{apikey.ScopeContentRead},
	})
	if err != nil {
		t.Fatal(err)
	}
	var blob string
	if err := st.DB().QueryRowContext(ctx, `SELECT group_concat(name||prefix||token_hash||scopes) FROM api_keys`).Scan(&blob); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(blob, raw) {
		t.Fatalf("raw key leaked into stored columns: %q", blob)
	}
}
