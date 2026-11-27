package webhook

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/store"
)

// Event names. The first four are subscribable (a webhook's events list); the
// test event is delivered on demand regardless of any subscription.
const (
	EventPostPublished   = "post.published"
	EventPostUpdated     = "post.updated"
	EventPostUnpublished = "post.unpublished"
	EventPostDeleted     = "post.deleted"
	EventTest            = "webhook.test"
)

const testMessage = "This is a test delivery from PocketPress."

// SubscribableEvents is the set of events a webhook may subscribe to (webhook
// .test is excluded — it is only ever sent by the "send test" action).
var SubscribableEvents = []string{
	EventPostPublished,
	EventPostUpdated,
	EventPostUnpublished,
	EventPostDeleted,
}

// IsSubscribableEvent reports whether event is one a webhook may subscribe to.
func IsSubscribableEvent(event string) bool {
	for _, e := range SubscribableEvents {
		if e == event {
			return true
		}
	}
	return false
}

// Dispatcher is the content.Events sink that turns post publish-state changes
// into durable delivery rows. On each event it inserts one webhook_deliveries
// row (attempt 0, pending, due now) per active webhook subscribed to that
// event; the worker delivers them asynchronously. It never blocks or fails the
// triggering request: every fan-out error is logged and swallowed.
//
// It deliberately does NOT decrypt secrets — enqueuing only writes the payload
// and metadata; the worker holds the Cipher and signs at delivery time.
type Dispatcher struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewDispatcher builds a Dispatcher over db. logger may be nil.
func NewDispatcher(db *sql.DB, logger *slog.Logger) *Dispatcher {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Dispatcher{db: db, logger: logger}
}

// Dispatcher implements content.Events.
var _ content.Events = (*Dispatcher)(nil)

func (d *Dispatcher) PostPublished(ctx context.Context, p *content.Post) {
	d.dispatchPost(ctx, EventPostPublished, p)
}

func (d *Dispatcher) PostUnpublished(ctx context.Context, p *content.Post) {
	d.dispatchPost(ctx, EventPostUnpublished, p)
}

func (d *Dispatcher) PostUpdated(ctx context.Context, p *content.Post) {
	d.dispatchPost(ctx, EventPostUpdated, p)
}

func (d *Dispatcher) PostDeleted(ctx context.Context, p *content.Post) {
	// The seam passes the pre-delete aggregate; the payload is built from it
	// directly and the DB is never re-read for the deleted row.
	d.dispatchPost(ctx, EventPostDeleted, p)
}

// envelope is the frozen webhook payload shape (development plan §8).
type envelope struct {
	ID        string `json:"id"`
	Event     string `json:"event"`
	CreatedAt string `json:"created_at"`
	Data      any    `json:"data"`
}

// postData is the data object for the four post events.
type postData struct {
	Post postPayload `json:"post"`
}

type postPayload struct {
	ID          string     `json:"id"`
	Slug        string     `json:"slug"`
	Status      string     `json:"status"`
	PublishedAt *time.Time `json:"published_at"`
}

// testData is the data object for webhook.test.
type testData struct {
	Webhook testWebhookRef `json:"webhook"`
	Message string         `json:"message"`
}

type testWebhookRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// dispatchPost fans a post event out to every active webhook subscribed to it.
func (d *Dispatcher) dispatchPost(ctx context.Context, event string, p *content.Post) {
	webhooks, err := store.ListActiveWebhooks(ctx, d.db)
	if err != nil {
		d.logger.Error("webhook dispatch: list active webhooks failed",
			slog.String("event", event), slog.String("error", err.Error()))
		return
	}
	data := postData{Post: postPayload{
		ID:          p.Row.ID,
		Slug:        p.Row.Slug,
		Status:      p.Row.Status,
		PublishedAt: p.Row.PublishedAt,
	}}
	for _, wh := range webhooks {
		if !containsEvent(wh.Events, event) {
			continue
		}
		if !MatchesFilters(wh.Filters, p) {
			continue
		}
		if _, err := d.enqueue(ctx, wh, event, data); err != nil {
			d.logger.Error("webhook dispatch: enqueue failed",
				slog.String("event", event), slog.String("webhook_id", wh.ID),
				slog.String("error", err.Error()))
		}
	}
}

// EnqueueTest enqueues a webhook.test delivery for one webhook, regardless of
// its event subscription (or active state — a test is an explicit admin
// action). Returns the new delivery row id, or store.ErrNotFound if the
// webhook does not exist.
func (d *Dispatcher) EnqueueTest(ctx context.Context, webhookID string) (string, error) {
	wh, err := store.GetWebhookByID(ctx, d.db, webhookID)
	if err != nil {
		return "", err
	}
	data := testData{
		Webhook: testWebhookRef{ID: wh.ID, Name: wh.Name},
		Message: testMessage,
	}
	return d.enqueue(ctx, wh, EventTest, data)
}

// enqueue builds the signed-body payload for one delivery and inserts its
// pending row (attempt 0, due now). Each row gets its own "evt_" event_id,
// which is both the payload id and the X-PocketPress-Delivery header value.
func (d *Dispatcher) enqueue(ctx context.Context, wh store.Webhook, event string, data any) (string, error) {
	id, err := store.NewID()
	if err != nil {
		return "", err
	}
	eventID := "evt_" + id

	payload, err := json.Marshal(envelope{
		ID:        eventID,
		Event:     event,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Data:      data,
	})
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	row, err := store.CreateDelivery(ctx, d.db, store.NewDelivery{
		WebhookID:     wh.ID,
		Event:         event,
		EventID:       eventID,
		Payload:       string(payload),
		Attempt:       0,
		Status:        store.DeliveryStatusPending,
		NextAttemptAt: &now,
	})
	if err != nil {
		return "", err
	}
	return row.ID, nil
}

func containsEvent(events []string, event string) bool {
	for _, e := range events {
		if e == event {
			return true
		}
	}
	return false
}
