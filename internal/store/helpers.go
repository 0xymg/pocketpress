package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// timeLayout is the on-disk timestamp format for every TEXT timestamp
// column: RFC3339 in UTC, per the frozen storage decision.
const timeLayout = time.RFC3339

// newID generates a new UUIDv7 primary key, PocketPress's frozen ID
// format: time-ordered (so PRIMARY KEY inserts stay roughly sequential
// instead of fragmenting a SQLite b-tree the way random UUIDv4 keys
// would), stored as TEXT.
func newID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return id.String(), nil
}

// NewID exposes newID to callers outside this package that must mint a
// PocketPress UUIDv7 before the row exists — e.g. the webhook dispatcher,
// which needs a delivery's "evt_" event_id to embed in the payload it then
// stores alongside it.
func NewID() (string, error) {
	return newID()
}

// formatTime renders t as PocketPress's on-disk timestamp format: RFC3339
// in UTC.
func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

// parseTime parses PocketPress's on-disk timestamp format.
func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", s, err)
	}
	return t, nil
}

// nullString converts an empty Go string to a SQL NULL and any other
// value to itself, for optional TEXT columns (avatar, excerpt, ...).
func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// stringOrEmpty converts a SQL NULL back to "", the inverse of
// nullString.
func stringOrEmpty(ns sql.NullString) string {
	return ns.String
}

// nullStringPtr converts a nil *string to a SQL NULL and a non-nil one to its
// value, for optional TEXT columns whose "unset" state is distinct from ""
// (webhook_deliveries.response_body: nil = never attempted, "" = an empty
// response body).
func nullStringPtr(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

// nullTime converts a nil *time.Time to a SQL NULL and a non-nil one to its
// RFC3339 UTC rendering, for the optional scheduled_for / published_at
// timestamp columns written by the publishing operations.
func nullTime(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: formatTime(*t), Valid: true}
}

// nullInt converts a nil *int to a SQL NULL and a non-nil one to its value,
// for optional INTEGER columns (media width/height).
func nullInt(n *int) sql.NullInt64 {
	if n == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*n), Valid: true}
}

// boolToInt renders a Go bool as the 0/1 SQLite stores for INTEGER
// boolean columns.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
