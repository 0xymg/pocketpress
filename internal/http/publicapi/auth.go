package publicapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/pocketpress/pocketpress/internal/apikey"
	"github.com/pocketpress/pocketpress/internal/store"
)

type ctxKey int

const (
	ctxAPIKey ctxKey = iota + 1
	ctxPrivateAuth
)

const privateCacheControl = "private, no-store"

// bearerToken extracts the raw Bearer credential from Authorization, or ""
// when absent / not Bearer.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "bearer "
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}

func withAPIKey(ctx context.Context, k store.APIKey) context.Context {
	return context.WithValue(ctx, ctxAPIKey, k)
}

func apiKeyFrom(ctx context.Context) (store.APIKey, bool) {
	k, ok := ctx.Value(ctxAPIKey).(store.APIKey)
	return k, ok
}

func withPrivateAuth(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxPrivateAuth, true)
}

func isPrivateAuth(ctx context.Context) bool {
	v, _ := ctx.Value(ctxPrivateAuth).(bool)
	return v
}

// resolveCacheControl swaps public cache directives for private, no-store
// when the request reached a gated endpoint via API key because the public
// API is disabled.
func resolveCacheControl(ctx context.Context, publicValue string) string {
	if isPrivateAuth(ctx) {
		return privateCacheControl
	}
	return publicValue
}

// authenticateAPIKey validates a presented ppkey_ Bearer token. Returns
// (zero, false, nil) when no API-key-shaped Bearer is present. Invalid keys
// yield ErrInvalidKey; missing scope yields ErrMissingScope.
func (h *Handler) authenticateAPIKey(ctx context.Context, r *http.Request, scope string) (store.APIKey, bool, error) {
	raw := bearerToken(r)
	if raw == "" || !apikey.LooksLikeKey(raw) {
		return store.APIKey{}, false, nil
	}
	if h.keys == nil {
		return store.APIKey{}, true, apikey.ErrInvalidKey
	}
	k, err := h.keys.RequireScope(ctx, raw, scope)
	if err != nil {
		return store.APIKey{}, true, err
	}
	return k, true, nil
}
