package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// APIKey is a row of the api_keys table. TokenHash is a SHA-256 hex digest of
// the raw key; the raw key is never stored. Prefix is a short display
// identifier derived from the raw key at creation.
type APIKey struct {
	ID         string
	Name       string
	Prefix     string
	TokenHash  string
	Scopes     []string
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NewAPIKey holds the fields CreateAPIKey writes. ID and timestamps are set
// by CreateAPIKey itself.
type NewAPIKey struct {
	Name      string
	Prefix    string
	TokenHash string
	Scopes    []string
	ExpiresAt *time.Time
}

// CreateAPIKey inserts an API key row and returns it as stored.
func CreateAPIKey(ctx context.Context, db DBTX, in NewAPIKey) (APIKey, error) {
	id, err := newID()
	if err != nil {
		return APIKey{}, err
	}
	scopes, err := marshalAPIKeyScopes(in.Scopes)
	if err != nil {
		return APIKey{}, err
	}

	now := time.Now().UTC()
	k := APIKey{
		ID:        id,
		Name:      in.Name,
		Prefix:    in.Prefix,
		TokenHash: in.TokenHash,
		Scopes:    in.Scopes,
		ExpiresAt: in.ExpiresAt,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO api_keys (
			id, name, prefix, token_hash, scopes,
			expires_at, last_used_at, revoked_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?)`,
		k.ID, k.Name, k.Prefix, k.TokenHash, scopes,
		nullTime(k.ExpiresAt), formatTime(k.CreatedAt), formatTime(k.UpdatedAt),
	)
	if err != nil {
		return APIKey{}, fmt.Errorf("insert api key: %w", err)
	}
	return k, nil
}

// GetAPIKeyByID looks up an API key by id. Returns ErrNotFound if no such row
// exists.
func GetAPIKeyByID(ctx context.Context, db DBTX, id string) (APIKey, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, name, prefix, token_hash, scopes,
			expires_at, last_used_at, revoked_at, created_at, updated_at
		FROM api_keys WHERE id = ?`, id)
	return scanAPIKey(row)
}

// GetAPIKeyByTokenHash looks up an API key by its token hash. Returns
// ErrNotFound if no such row exists. Callers must still check revoked/
// expired state.
func GetAPIKeyByTokenHash(ctx context.Context, db DBTX, tokenHash string) (APIKey, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, name, prefix, token_hash, scopes,
			expires_at, last_used_at, revoked_at, created_at, updated_at
		FROM api_keys WHERE token_hash = ?`, tokenHash)
	return scanAPIKey(row)
}

// ListAPIKeys returns every API key, oldest first (created_at ASC, id ASC).
func ListAPIKeys(ctx context.Context, db DBTX) ([]APIKey, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, prefix, token_hash, scopes,
			expires_at, last_used_at, revoked_at, created_at, updated_at
		FROM api_keys ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query api keys: %w", err)
	}
	return scanAPIKeys(rows)
}

// RevokeAPIKey sets revoked_at (and updated_at) when the key is not already
// revoked. Returns ErrNotFound if no such row exists.
func RevokeAPIKey(ctx context.Context, db DBTX, id string, at time.Time) error {
	res, err := db.ExecContext(ctx, `
		UPDATE api_keys
		SET revoked_at = COALESCE(revoked_at, ?), updated_at = ?
		WHERE id = ?`,
		formatTime(at.UTC()), formatTime(at.UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	return requireRowsAffected(res)
}

// TouchAPIKeyLastUsed updates last_used_at (and updated_at) for a key.
// Returns ErrNotFound if no such row exists.
func TouchAPIKeyLastUsed(ctx context.Context, db DBTX, id string, at time.Time) error {
	res, err := db.ExecContext(ctx, `
		UPDATE api_keys SET last_used_at = ?, updated_at = ? WHERE id = ?`,
		formatTime(at.UTC()), formatTime(at.UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("touch api key last_used_at: %w", err)
	}
	return requireRowsAffected(res)
}

// DeleteAPIKey hard-deletes an API key row. Returns ErrNotFound if no such
// row exists.
func DeleteAPIKey(ctx context.Context, db DBTX, id string) error {
	res, err := db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	return requireRowsAffected(res)
}

func scanAPIKeys(rows *sql.Rows) ([]APIKey, error) {
	defer rows.Close()
	keys := make([]APIKey, 0)
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api keys: %w", err)
	}
	return keys, nil
}

func scanAPIKey(row rowScanner) (APIKey, error) {
	var (
		k          APIKey
		scopes     string
		expiresAt  sql.NullString
		lastUsedAt sql.NullString
		revokedAt  sql.NullString
		createdAt  string
		updatedAt  string
	)
	err := row.Scan(
		&k.ID, &k.Name, &k.Prefix, &k.TokenHash, &scopes,
		&expiresAt, &lastUsedAt, &revokedAt, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return APIKey{}, ErrNotFound
	}
	if err != nil {
		return APIKey{}, fmt.Errorf("scan api key: %w", err)
	}

	if k.Scopes, err = unmarshalAPIKeyScopes(scopes); err != nil {
		return APIKey{}, err
	}
	if k.ExpiresAt, err = parseTimePtr(expiresAt); err != nil {
		return APIKey{}, err
	}
	if k.LastUsedAt, err = parseTimePtr(lastUsedAt); err != nil {
		return APIKey{}, err
	}
	if k.RevokedAt, err = parseTimePtr(revokedAt); err != nil {
		return APIKey{}, err
	}
	if k.CreatedAt, err = parseTime(createdAt); err != nil {
		return APIKey{}, err
	}
	if k.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return APIKey{}, err
	}
	return k, nil
}

func marshalAPIKeyScopes(scopes []string) (string, error) {
	if scopes == nil {
		scopes = []string{}
	}
	b, err := json.Marshal(scopes)
	if err != nil {
		return "", fmt.Errorf("marshal api key scopes: %w", err)
	}
	return string(b), nil
}

func unmarshalAPIKeyScopes(raw string) ([]string, error) {
	var scopes []string
	if err := json.Unmarshal([]byte(raw), &scopes); err != nil {
		return nil, fmt.Errorf("unmarshal api key scopes: %w", err)
	}
	return scopes, nil
}
