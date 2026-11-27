package apikey

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/store"
)

// Supported scopes (plan §5 api_keys).
const (
	ScopeContentRead = "content:read"
	ScopePreviewRead = "preview:read"
)

var allowedScopes = map[string]struct{}{
	ScopeContentRead: {},
	ScopePreviewRead: {},
}

const (
	keyNameMax = 200

	nameRequiredMessage   = "Name is required."
	nameTooLongMessage    = "Name must be at most 200 characters."
	scopesRequiredMessage = "At least one scope is required."
	scopesInvalidMessage  = "Scopes must be a non-empty subset of: content:read, preview:read."
	expiresPastMessage    = "Expires at must be in the future."
)

// ErrInvalidKey is returned when a presented Bearer token is not a usable
// API key (unknown, revoked, or expired).
var ErrInvalidKey = errors.New("apikey: invalid or revoked key")

// ErrMissingScope is returned when a key is valid but lacks a required scope.
var ErrMissingScope = errors.New("apikey: missing required scope")

// Service is the API-key domain layer over the store.
type Service struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewService builds an API-key Service. logger may be nil.
func NewService(db *sql.DB, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{db: db, logger: logger}
}

// CreateInput is the resolved create body for an API key.
type CreateInput struct {
	Name      string
	Scopes    []string
	ExpiresAt *time.Time
}

// List returns every API key, oldest first.
func (s *Service) List(ctx context.Context) ([]store.APIKey, error) {
	return store.ListAPIKeys(ctx, s.db)
}

// Get returns a single API key. Returns store.ErrNotFound for an unknown id.
func (s *Service) Get(ctx context.Context, id string) (store.APIKey, error) {
	return store.GetAPIKeyByID(ctx, s.db, id)
}

// Create validates and inserts an API key, returning the stored row and the
// raw key (shown exactly once). On validation failure it returns a
// *content.ValidationError.
func (s *Service) Create(ctx context.Context, in CreateInput) (store.APIKey, string, error) {
	fields := map[string]string{}
	name := validateName(in.Name, fields)
	scopes := validateScopes(in.Scopes, fields)
	expiresAt := validateExpiresAt(in.ExpiresAt, time.Now().UTC(), fields)
	if len(fields) > 0 {
		return store.APIKey{}, "", &content.ValidationError{Fields: fields}
	}

	raw, err := GenerateKey()
	if err != nil {
		return store.APIKey{}, "", err
	}

	k, err := store.CreateAPIKey(ctx, s.db, store.NewAPIKey{
		Name:      name,
		Prefix:    DisplayPrefix(raw),
		TokenHash: Hash(raw),
		Scopes:    scopes,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return store.APIKey{}, "", err
	}
	s.logger.Info("api key created", slog.String("api_key_id", k.ID), slog.String("prefix", k.Prefix))
	return k, raw, nil
}

// Revoke soft-revokes an API key (sets revoked_at). Idempotent if already
// revoked. Returns store.ErrNotFound for an unknown id.
func (s *Service) Revoke(ctx context.Context, id string) (store.APIKey, error) {
	if _, err := store.GetAPIKeyByID(ctx, s.db, id); err != nil {
		return store.APIKey{}, err
	}
	if err := store.RevokeAPIKey(ctx, s.db, id, time.Now().UTC()); err != nil {
		return store.APIKey{}, err
	}
	s.logger.Info("api key revoked", slog.String("api_key_id", id))
	return store.GetAPIKeyByID(ctx, s.db, id)
}

// Delete hard-deletes an API key. Returns store.ErrNotFound for an unknown id.
func (s *Service) Delete(ctx context.Context, id string) error {
	if err := store.DeleteAPIKey(ctx, s.db, id); err != nil {
		return err
	}
	s.logger.Info("api key deleted", slog.String("api_key_id", id))
	return nil
}

// Authenticate verifies a raw Bearer token as an API key that is not
// revoked or expired. On success it best-effort updates last_used_at and
// returns the key row. Missing/invalid/revoked/expired keys yield
// ErrInvalidKey (never store.ErrNotFound) so callers cannot distinguish
// "unknown" from "revoked".
func (s *Service) Authenticate(ctx context.Context, raw string) (store.APIKey, error) {
	raw = strings.TrimSpace(raw)
	if !LooksLikeKey(raw) {
		return store.APIKey{}, ErrInvalidKey
	}
	k, err := store.GetAPIKeyByTokenHash(ctx, s.db, Hash(raw))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.APIKey{}, ErrInvalidKey
		}
		return store.APIKey{}, err
	}
	now := time.Now().UTC()
	if k.RevokedAt != nil {
		return store.APIKey{}, ErrInvalidKey
	}
	if k.ExpiresAt != nil && !k.ExpiresAt.After(now) {
		return store.APIKey{}, ErrInvalidKey
	}
	// Best-effort last-used stamp; auth still succeeds if the touch fails.
	if err := store.TouchAPIKeyLastUsed(ctx, s.db, k.ID, now); err != nil {
		s.logger.Warn("api key last_used_at update failed",
			slog.String("api_key_id", k.ID),
			slog.String("error", err.Error()))
	}
	k.LastUsedAt = &now
	return k, nil
}

// HasScope reports whether k includes scope.
func HasScope(k store.APIKey, scope string) bool {
	for _, s := range k.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// RequireScope authenticates raw and checks scope. Combines Authenticate +
// HasScope for handlers.
func (s *Service) RequireScope(ctx context.Context, raw, scope string) (store.APIKey, error) {
	k, err := s.Authenticate(ctx, raw)
	if err != nil {
		return store.APIKey{}, err
	}
	if !HasScope(k, scope) {
		return store.APIKey{}, ErrMissingScope
	}
	return k, nil
}

func validateName(name string, fields map[string]string) string {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		fields["name"] = nameRequiredMessage
	case utf8.RuneCountInString(name) > keyNameMax:
		fields["name"] = nameTooLongMessage
	}
	return name
}

func validateScopes(scopes []string, fields map[string]string) []string {
	if len(scopes) == 0 {
		fields["scopes"] = scopesRequiredMessage
		return nil
	}
	seen := make(map[string]struct{}, len(scopes))
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if _, ok := allowedScopes[s]; !ok {
			fields["scopes"] = scopesInvalidMessage
			return nil
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		fields["scopes"] = scopesRequiredMessage
		return nil
	}
	return out
}

func validateExpiresAt(expiresAt *time.Time, now time.Time, fields map[string]string) *time.Time {
	if expiresAt == nil {
		return nil
	}
	t := expiresAt.UTC()
	if !t.After(now) {
		fields["expires_at"] = expiresPastMessage
		return nil
	}
	return &t
}
