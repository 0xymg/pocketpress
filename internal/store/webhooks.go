package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Webhook is a row of the webhooks table: a subscription endpoint plus its
// encrypted signing secret and the event names it wants delivered.
type Webhook struct {
	ID              string
	Name            string
	URL             string
	SecretEncrypted string // AES-256-GCM ciphertext, base64; never the raw secret
	Events          []string
	Active          bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// NewWebhook holds the fields CreateWebhook writes. ID and timestamps are set
// by CreateWebhook itself.
type NewWebhook struct {
	Name            string
	URL             string
	SecretEncrypted string
	Events          []string
	Active          bool
}

// CreateWebhook inserts a webhook row and returns it as stored.
func CreateWebhook(ctx context.Context, db DBTX, in NewWebhook) (Webhook, error) {
	id, err := newID()
	if err != nil {
		return Webhook{}, err
	}
	events, err := marshalEvents(in.Events)
	if err != nil {
		return Webhook{}, err
	}

	now := time.Now().UTC()
	w := Webhook{
		ID:              id,
		Name:            in.Name,
		URL:             in.URL,
		SecretEncrypted: in.SecretEncrypted,
		Events:          in.Events,
		Active:          in.Active,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO webhooks (id, name, url, secret_encrypted, events, active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.Name, w.URL, w.SecretEncrypted, events, boolToInt(w.Active),
		formatTime(w.CreatedAt), formatTime(w.UpdatedAt),
	)
	if err != nil {
		return Webhook{}, fmt.Errorf("insert webhook: %w", err)
	}
	return w, nil
}

// GetWebhookByID looks up a webhook by id. Returns ErrNotFound if no such row
// exists.
func GetWebhookByID(ctx context.Context, db DBTX, id string) (Webhook, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, name, url, secret_encrypted, events, active, created_at, updated_at
		FROM webhooks WHERE id = ?`, id)
	return scanWebhook(row)
}

// ListWebhooks returns every webhook, oldest first (created_at ASC, id ASC
// tiebreak for a stable order).
func ListWebhooks(ctx context.Context, db DBTX) ([]Webhook, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, url, secret_encrypted, events, active, created_at, updated_at
		FROM webhooks ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query webhooks: %w", err)
	}
	return scanWebhooks(rows)
}

// ListActiveWebhooks returns every active webhook. The dispatcher filters the
// result by event membership in Go (the events column is a JSON array, so a
// per-event SQL predicate would need the JSON1 extension); the set of
// webhooks is small, so an in-memory filter is the boring, portable choice.
func ListActiveWebhooks(ctx context.Context, db DBTX) ([]Webhook, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, url, secret_encrypted, events, active, created_at, updated_at
		FROM webhooks WHERE active = 1 ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query active webhooks: %w", err)
	}
	return scanWebhooks(rows)
}

// WebhookUpdate holds the fully-resolved writable fields for UpdateWebhook
// (the service loads the existing row, applies the partial patch, and passes
// the final values here).
type WebhookUpdate struct {
	Name            string
	URL             string
	SecretEncrypted string
	Events          []string
	Active          bool
}

// UpdateWebhook overwrites a webhook's editable columns and bumps updated_at.
// Returns ErrNotFound if no such row exists.
func UpdateWebhook(ctx context.Context, db DBTX, id string, in WebhookUpdate) error {
	events, err := marshalEvents(in.Events)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `
		UPDATE webhooks SET name = ?, url = ?, secret_encrypted = ?, events = ?, active = ?, updated_at = ?
		WHERE id = ?`,
		in.Name, in.URL, in.SecretEncrypted, events, boolToInt(in.Active),
		formatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("update webhook: %w", err)
	}
	return requireRowsAffected(res)
}

// SetWebhookSecret overwrites only a webhook's encrypted secret (and bumps
// updated_at) — the write path for secret rotation. Returns ErrNotFound if no
// such row exists.
func SetWebhookSecret(ctx context.Context, db DBTX, id, secretEncrypted string) error {
	res, err := db.ExecContext(ctx, `
		UPDATE webhooks SET secret_encrypted = ?, updated_at = ? WHERE id = ?`,
		secretEncrypted, formatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("set webhook secret: %w", err)
	}
	return requireRowsAffected(res)
}

// DeleteWebhook removes a webhook. Its webhook_deliveries rows cascade away
// (ON DELETE CASCADE in 0001_init, enforced because store.Open enables the
// foreign_keys pragma). Returns ErrNotFound if no such row exists.
func DeleteWebhook(ctx context.Context, db DBTX, id string) error {
	res, err := db.ExecContext(ctx, `DELETE FROM webhooks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	return requireRowsAffected(res)
}

func scanWebhooks(rows *sql.Rows) ([]Webhook, error) {
	defer rows.Close()
	webhooks := make([]Webhook, 0)
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		webhooks = append(webhooks, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate webhooks: %w", err)
	}
	return webhooks, nil
}

func scanWebhook(row rowScanner) (Webhook, error) {
	var (
		w         Webhook
		events    string
		active    int
		createdAt string
		updatedAt string
	)
	err := row.Scan(&w.ID, &w.Name, &w.URL, &w.SecretEncrypted, &events, &active, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Webhook{}, ErrNotFound
	}
	if err != nil {
		return Webhook{}, fmt.Errorf("scan webhook: %w", err)
	}

	w.Active = active != 0
	if w.Events, err = unmarshalEvents(events); err != nil {
		return Webhook{}, err
	}
	if w.CreatedAt, err = parseTime(createdAt); err != nil {
		return Webhook{}, err
	}
	if w.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Webhook{}, err
	}
	return w, nil
}

// marshalEvents renders an events slice as the JSON array text stored in the
// events column; a nil slice becomes "[]".
func marshalEvents(events []string) (string, error) {
	if events == nil {
		events = []string{}
	}
	b, err := json.Marshal(events)
	if err != nil {
		return "", fmt.Errorf("marshal webhook events: %w", err)
	}
	return string(b), nil
}

func unmarshalEvents(raw string) ([]string, error) {
	var events []string
	if err := json.Unmarshal([]byte(raw), &events); err != nil {
		return nil, fmt.Errorf("unmarshal webhook events: %w", err)
	}
	return events, nil
}
