package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Webhook delivery statuses. The full set is constrained by the
// webhook_deliveries.status CHECK in 0001_init.
const (
	DeliveryStatusPending = "pending"
	DeliveryStatusSuccess = "success"
	DeliveryStatusFailed  = "failed"
)

// Delivery is a row of the webhook_deliveries table: one attempt-tracked
// delivery of one event to one webhook. The DB-backed row IS the queue, so a
// pending delivery survives a process restart.
type Delivery struct {
	ID            string
	WebhookID     string
	Event         string
	EventID       string // "evt_<uuidv7>", unique per row; the X-PocketPress-Delivery id
	Payload       string // the exact JSON body that is (HMAC-)signed and POSTed
	Attempt       int    // attempts made so far; 0 until the worker first tries
	Status        string
	ResponseCode  *int
	ResponseBody  *string
	NextAttemptAt *time.Time
	DeliveredAt   *time.Time
	CreatedAt     time.Time
}

// NewDelivery holds the fields CreateDelivery writes. ID and created_at are
// set by CreateDelivery; the caller supplies the pre-built EventID and Payload
// (the payload embeds the same EventID) and the initial Attempt/Status/
// NextAttemptAt (the dispatcher enqueues attempt 0, pending, due now).
type NewDelivery struct {
	WebhookID     string
	Event         string
	EventID       string
	Payload       string
	Attempt       int
	Status        string
	NextAttemptAt *time.Time
}

// CreateDelivery inserts a delivery row and returns it as stored.
func CreateDelivery(ctx context.Context, db DBTX, in NewDelivery) (Delivery, error) {
	id, err := newID()
	if err != nil {
		return Delivery{}, err
	}

	now := time.Now().UTC()
	d := Delivery{
		ID:            id,
		WebhookID:     in.WebhookID,
		Event:         in.Event,
		EventID:       in.EventID,
		Payload:       in.Payload,
		Attempt:       in.Attempt,
		Status:        in.Status,
		NextAttemptAt: in.NextAttemptAt,
		CreatedAt:     now,
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO webhook_deliveries (
			id, webhook_id, event, event_id, payload, attempt, status,
			response_code, response_body, next_attempt_at, delivered_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, NULL, ?)`,
		d.ID, d.WebhookID, d.Event, d.EventID, d.Payload, d.Attempt, d.Status,
		nullTime(d.NextAttemptAt), formatTime(d.CreatedAt),
	)
	if err != nil {
		return Delivery{}, fmt.Errorf("insert webhook delivery: %w", err)
	}
	return d, nil
}

// GetDeliveryByID looks up a delivery by id. Returns ErrNotFound if no such
// row exists.
func GetDeliveryByID(ctx context.Context, db DBTX, id string) (Delivery, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, webhook_id, event, event_id, payload, attempt, status,
			response_code, response_body, next_attempt_at, delivered_at, created_at
		FROM webhook_deliveries WHERE id = ?`, id)
	return scanDelivery(row)
}

// ListDeliveriesForWebhook returns one page of a webhook's deliveries, newest
// first (created_at DESC, id DESC tiebreak), limit/offset paginated.
func ListDeliveriesForWebhook(ctx context.Context, db DBTX, webhookID string, limit, offset int) ([]Delivery, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, webhook_id, event, event_id, payload, attempt, status,
			response_code, response_body, next_attempt_at, delivered_at, created_at
		FROM webhook_deliveries
		WHERE webhook_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?`, webhookID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query webhook deliveries: %w", err)
	}
	return scanDeliveries(rows)
}

// CountDeliveriesForWebhook returns how many deliveries a webhook has, for
// list pagination metadata.
func CountDeliveriesForWebhook(ctx context.Context, db DBTX, webhookID string) (int, error) {
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM webhook_deliveries WHERE webhook_id = ?`, webhookID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count webhook deliveries: %w", err)
	}
	return count, nil
}

// ListDueDeliveries returns pending deliveries whose next_attempt_at is at or
// before now, oldest-due first, capped at limit. It is the query behind the
// delivery worker's every-5s pass.
func ListDueDeliveries(ctx context.Context, db DBTX, now time.Time, limit int) ([]Delivery, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, webhook_id, event, event_id, payload, attempt, status,
			response_code, response_body, next_attempt_at, delivered_at, created_at
		FROM webhook_deliveries
		WHERE status = ? AND next_attempt_at IS NOT NULL AND next_attempt_at <= ?
		ORDER BY next_attempt_at ASC, id ASC
		LIMIT ?`, DeliveryStatusPending, formatTime(now), limit)
	if err != nil {
		return nil, fmt.Errorf("query due webhook deliveries: %w", err)
	}
	return scanDeliveries(rows)
}

// DeliveryResult is the outcome of one delivery attempt the worker records.
type DeliveryResult struct {
	Status        string
	Attempt       int
	ResponseCode  *int
	ResponseBody  *string
	NextAttemptAt *time.Time
	DeliveredAt   *time.Time
}

// UpdateDeliveryResult writes the outcome of a delivery attempt. Returns
// ErrNotFound if no such row exists.
func UpdateDeliveryResult(ctx context.Context, db DBTX, id string, r DeliveryResult) error {
	res, err := db.ExecContext(ctx, `
		UPDATE webhook_deliveries SET
			status = ?, attempt = ?, response_code = ?, response_body = ?,
			next_attempt_at = ?, delivered_at = ?
		WHERE id = ?`,
		r.Status, r.Attempt, nullInt(r.ResponseCode), nullStringPtr(r.ResponseBody),
		nullTime(r.NextAttemptAt), nullTime(r.DeliveredAt), id,
	)
	if err != nil {
		return fmt.Errorf("update webhook delivery result: %w", err)
	}
	return requireRowsAffected(res)
}

// RequeueDelivery moves a delivery back to pending, due at nextAttemptAt,
// without touching its attempt counter or last-response fields — the storage
// side of a manual retry (the next worker pass makes one more attempt, which
// increments attempt from where it left off). Returns ErrNotFound if no such
// row exists.
func RequeueDelivery(ctx context.Context, db DBTX, id string, nextAttemptAt time.Time) error {
	res, err := db.ExecContext(ctx, `
		UPDATE webhook_deliveries SET status = ?, next_attempt_at = ? WHERE id = ?`,
		DeliveryStatusPending, formatTime(nextAttemptAt), id,
	)
	if err != nil {
		return fmt.Errorf("requeue webhook delivery: %w", err)
	}
	return requireRowsAffected(res)
}

func scanDeliveries(rows *sql.Rows) ([]Delivery, error) {
	defer rows.Close()
	deliveries := make([]Delivery, 0)
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate webhook deliveries: %w", err)
	}
	return deliveries, nil
}

func scanDelivery(row rowScanner) (Delivery, error) {
	var (
		d             Delivery
		responseCode  sql.NullInt64
		responseBody  sql.NullString
		nextAttemptAt sql.NullString
		deliveredAt   sql.NullString
		createdAt     string
	)
	err := row.Scan(
		&d.ID, &d.WebhookID, &d.Event, &d.EventID, &d.Payload, &d.Attempt, &d.Status,
		&responseCode, &responseBody, &nextAttemptAt, &deliveredAt, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Delivery{}, ErrNotFound
	}
	if err != nil {
		return Delivery{}, fmt.Errorf("scan webhook delivery: %w", err)
	}

	if responseCode.Valid {
		c := int(responseCode.Int64)
		d.ResponseCode = &c
	}
	if responseBody.Valid {
		b := responseBody.String
		d.ResponseBody = &b
	}
	if d.NextAttemptAt, err = parseTimePtr(nextAttemptAt); err != nil {
		return Delivery{}, err
	}
	if d.DeliveredAt, err = parseTimePtr(deliveredAt); err != nil {
		return Delivery{}, err
	}
	if d.CreatedAt, err = parseTime(createdAt); err != nil {
		return Delivery{}, err
	}
	return d, nil
}
