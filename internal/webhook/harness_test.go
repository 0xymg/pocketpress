package webhook

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

// newTestDB opens a fresh migrated temp-dir store for a test.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	if _, err := store.Up(context.Background(), st.DB()); err != nil {
		t.Fatalf("store.Up() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st.DB()
}

// testCipher builds a Cipher over a fixed key, so encrypt and decrypt match
// across a test's service/worker.
func testCipher() *Cipher {
	return NewCipher(DeriveKey([]byte("webhook-test-encryption-key")))
}

// insertWebhook inserts a webhook row directly with a dummy (non-decryptable
// unless the caller uses testCipher's plaintext) secret — enough for tests that
// only exercise dispatch/enqueue, which never decrypt.
func insertWebhook(t *testing.T, db *sql.DB, name, url string, events []string, active bool) store.Webhook {
	t.Helper()
	wh, err := store.CreateWebhook(context.Background(), db, store.NewWebhook{
		Name:            name,
		URL:             url,
		SecretEncrypted: "ciphertext-placeholder",
		Events:          events,
		Active:          active,
	})
	if err != nil {
		t.Fatalf("CreateWebhook(%q) error = %v", name, err)
	}
	return wh
}

// deliveries returns every delivery row for a webhook, newest first.
func deliveries(t *testing.T, db *sql.DB, webhookID string) []store.Delivery {
	t.Helper()
	rows, err := store.ListDeliveriesForWebhook(context.Background(), db, webhookID, 100, 0)
	if err != nil {
		t.Fatalf("ListDeliveriesForWebhook() error = %v", err)
	}
	return rows
}

// getDelivery reloads a delivery row by id.
func getDelivery(t *testing.T, db *sql.DB, id string) store.Delivery {
	t.Helper()
	d, err := store.GetDeliveryByID(context.Background(), db, id)
	if err != nil {
		t.Fatalf("GetDeliveryByID(%s) error = %v", id, err)
	}
	return d
}

// ptrTime returns a pointer to t, for building fixed timestamps in payloads.
func ptrTime(t time.Time) *time.Time { return &t }
