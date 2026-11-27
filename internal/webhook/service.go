package webhook

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

// webhookNameMax caps a webhook name (ours; the column itself is unconstrained).
const webhookNameMax = 200

// Validation messages.
const (
	nameRequiredMessage   = "Name is required."
	nameTooLongMessage    = "Name must be at most 200 characters."
	eventsInvalidMessage  = "Events must be a subset of: post.published, post.updated, post.unpublished, post.deleted."
	categoryFilterMessage = "One or more category_ids do not exist."
	tagFilterMessage      = "One or more tag_ids do not exist."
)

// ErrDeliveryNotFailed is returned by RetryDelivery when the delivery is not
// in the failed state (only failed deliveries can be manually retried). The
// admin API maps it to a 400.
var ErrDeliveryNotFailed = errors.New("webhook: delivery is not in the failed state")

// Service is the webhook domain layer over the store. It owns webhook CRUD and
// validation, signing-secret generation/encryption, the "send test" and manual
// retry actions, and delivery-history listing. Handlers validate shape, call a
// method, and serialize the result.
type Service struct {
	db         *sql.DB
	cipher     *Cipher
	dispatcher *Dispatcher
	production bool
	logger     *slog.Logger
}

// NewService builds a webhook Service. cipher encrypts new/rotated secrets;
// dispatcher enqueues test deliveries; production toggles the strict URL
// validation (https + SSRF host rules). logger may be nil.
func NewService(db *sql.DB, cipher *Cipher, dispatcher *Dispatcher, production bool, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{db: db, cipher: cipher, dispatcher: dispatcher, production: production, logger: logger}
}

// CreateInput is the resolved create body for a webhook. Active defaults to
// true at the HTTP layer when the field is absent. Filters default to empty
// (match all posts) when omitted.
type CreateInput struct {
	Name    string
	URL     string
	Events  []string
	Filters store.WebhookFilters
	Active  bool
}

// UpdatePatch is the partial-update body: each nil field is left unchanged.
type UpdatePatch struct {
	Name    *string
	URL     *string
	Events  *[]string
	Filters *store.WebhookFilters
	Active  *bool
}

// List returns every webhook, oldest first.
func (s *Service) List(ctx context.Context) ([]store.Webhook, error) {
	return store.ListWebhooks(ctx, s.db)
}

// Get returns a single webhook. Returns store.ErrNotFound for an unknown id.
func (s *Service) Get(ctx context.Context, id string) (store.Webhook, error) {
	return store.GetWebhookByID(ctx, s.db, id)
}

// Create validates and inserts a webhook, generating and encrypting a fresh
// signing secret. It returns the stored webhook and the raw secret, which the
// caller shows exactly once. On validation failure it returns a
// *content.ValidationError.
func (s *Service) Create(ctx context.Context, in CreateInput) (store.Webhook, string, error) {
	fields := map[string]string{}
	name := validateName(in.Name, fields)
	url := validateURL(in.URL, s.production, fields)
	events := validateEvents(in.Events, fields)
	filters := s.validateFilters(ctx, in.Filters, fields)
	if len(fields) > 0 {
		return store.Webhook{}, "", &content.ValidationError{Fields: fields}
	}

	secret, err := GenerateSecret()
	if err != nil {
		return store.Webhook{}, "", err
	}
	encrypted, err := s.cipher.Encrypt(secret)
	if err != nil {
		return store.Webhook{}, "", err
	}

	wh, err := store.CreateWebhook(ctx, s.db, store.NewWebhook{
		Name:            name,
		URL:             url,
		SecretEncrypted: encrypted,
		Events:          events,
		Filters:         filters,
		Active:          in.Active,
	})
	if err != nil {
		return store.Webhook{}, "", err
	}
	s.logger.Info("webhook created", slog.String("webhook_id", wh.ID))
	return wh, secret, nil
}

// Update applies a partial update. Returns store.ErrNotFound for an unknown id
// and a *content.ValidationError on validation failure. The secret is never
// touched here (use RotateSecret).
func (s *Service) Update(ctx context.Context, id string, patch UpdatePatch) (store.Webhook, error) {
	existing, err := store.GetWebhookByID(ctx, s.db, id)
	if err != nil {
		return store.Webhook{}, err
	}

	fields := map[string]string{}
	name := existing.Name
	if patch.Name != nil {
		name = validateName(*patch.Name, fields)
	}
	url := existing.URL
	if patch.URL != nil {
		url = validateURL(*patch.URL, s.production, fields)
	}
	events := existing.Events
	if patch.Events != nil {
		events = validateEvents(*patch.Events, fields)
	}
	filters := existing.Filters
	if patch.Filters != nil {
		filters = s.validateFilters(ctx, *patch.Filters, fields)
	}
	active := existing.Active
	if patch.Active != nil {
		active = *patch.Active
	}
	if len(fields) > 0 {
		return store.Webhook{}, &content.ValidationError{Fields: fields}
	}

	if err := store.UpdateWebhook(ctx, s.db, id, store.WebhookUpdate{
		Name:            name,
		URL:             url,
		SecretEncrypted: existing.SecretEncrypted,
		Events:          events,
		Filters:         filters,
		Active:          active,
	}); err != nil {
		return store.Webhook{}, err
	}
	s.logger.Info("webhook updated", slog.String("webhook_id", id))
	return store.GetWebhookByID(ctx, s.db, id)
}

// RotateSecret generates a new signing secret for a webhook, stores its
// ciphertext and returns the raw value (shown once). Returns store.ErrNotFound
// for an unknown id.
func (s *Service) RotateSecret(ctx context.Context, id string) (string, error) {
	secret, err := GenerateSecret()
	if err != nil {
		return "", err
	}
	encrypted, err := s.cipher.Encrypt(secret)
	if err != nil {
		return "", err
	}
	if err := store.SetWebhookSecret(ctx, s.db, id, encrypted); err != nil {
		return "", err
	}
	s.logger.Info("webhook secret rotated", slog.String("webhook_id", id))
	return secret, nil
}

// Delete removes a webhook (its deliveries cascade). Returns store.ErrNotFound
// for an unknown id.
func (s *Service) Delete(ctx context.Context, id string) error {
	if err := store.DeleteWebhook(ctx, s.db, id); err != nil {
		return err
	}
	s.logger.Info("webhook deleted", slog.String("webhook_id", id))
	return nil
}

// SendTest enqueues a webhook.test delivery for a webhook, returning the new
// delivery id. Returns store.ErrNotFound for an unknown id.
func (s *Service) SendTest(ctx context.Context, id string) (string, error) {
	return s.dispatcher.EnqueueTest(ctx, id)
}

// DeliveryPage is one page of a webhook's delivery history.
type DeliveryPage struct {
	Deliveries []store.Delivery
	Page       int
	PerPage    int
	TotalItems int
	TotalPages int
}

// ListDeliveries returns one page of a webhook's deliveries, newest first.
// Returns store.ErrNotFound if the webhook does not exist.
func (s *Service) ListDeliveries(ctx context.Context, webhookID string, page, perPage int) (DeliveryPage, error) {
	if _, err := store.GetWebhookByID(ctx, s.db, webhookID); err != nil {
		return DeliveryPage{}, err
	}

	total, err := store.CountDeliveriesForWebhook(ctx, s.db, webhookID)
	if err != nil {
		return DeliveryPage{}, err
	}
	offset := (page - 1) * perPage
	rows, err := store.ListDeliveriesForWebhook(ctx, s.db, webhookID, perPage, offset)
	if err != nil {
		return DeliveryPage{}, err
	}
	totalPages := 0
	if perPage > 0 {
		totalPages = (total + perPage - 1) / perPage
	}
	return DeliveryPage{
		Deliveries: rows,
		Page:       page,
		PerPage:    perPage,
		TotalItems: total,
		TotalPages: totalPages,
	}, nil
}

// RetryDelivery re-queues a failed delivery for one more attempt: status
// pending, due now, attempt counter unchanged (the next attempt continues from
// where it left off; if it fails again the delivery returns to failed). Returns
// store.ErrNotFound for an unknown id and ErrDeliveryNotFailed if the delivery
// is not currently failed.
func (s *Service) RetryDelivery(ctx context.Context, deliveryID string) error {
	d, err := store.GetDeliveryByID(ctx, s.db, deliveryID)
	if err != nil {
		return err
	}
	if d.Status != store.DeliveryStatusFailed {
		return ErrDeliveryNotFailed
	}
	if err := store.RequeueDelivery(ctx, s.db, deliveryID, time.Now().UTC()); err != nil {
		return err
	}
	s.logger.Info("webhook delivery re-queued", slog.String("delivery_id", deliveryID))
	return nil
}

// validateName trims and length-checks a webhook name, recording a "name"
// field error if invalid, and returns the trimmed value.
func validateName(raw string, fields map[string]string) string {
	name := strings.TrimSpace(raw)
	switch {
	case name == "":
		fields["name"] = nameRequiredMessage
	case utf8.RuneCountInString(name) > webhookNameMax:
		fields["name"] = nameTooLongMessage
	}
	return name
}

// validateURL trims and validates a webhook URL (http(s), plus https + SSRF
// host rules in production), recording a "url" field error if invalid, and
// returns the trimmed value.
func validateURL(raw string, production bool, fields map[string]string) string {
	url := strings.TrimSpace(raw)
	if err := ValidateWebhookURL(url, production); err != nil {
		fields["url"] = err.Error()
	}
	return url
}

// validateEvents checks each event is subscribable (recording an "events"
// field error otherwise) and returns the de-duplicated list. An empty list is
// valid — such a webhook simply never auto-fires (but can still be tested).
func validateEvents(events []string, fields map[string]string) []string {
	seen := make(map[string]bool, len(events))
	out := make([]string, 0, len(events))
	for _, e := range events {
		e = strings.TrimSpace(e)
		if !IsSubscribableEvent(e) {
			fields["events"] = eventsInvalidMessage
			continue
		}
		if seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

// validateFilters de-duplicates ids, confirms every category/tag id exists,
// and records field errors under "filters" / "category_ids" / "tag_ids".
func (s *Service) validateFilters(ctx context.Context, in store.WebhookFilters, fields map[string]string) store.WebhookFilters {
	cats := dedupeIDs(in.CategoryIDs)
	tags := dedupeIDs(in.TagIDs)

	for _, id := range cats {
		if id == "" {
			fields["category_ids"] = categoryFilterMessage
			continue
		}
		if _, err := store.GetCategoryByID(ctx, s.db, id); err != nil {
			fields["category_ids"] = categoryFilterMessage
			break
		}
	}
	for _, id := range tags {
		if id == "" {
			fields["tag_ids"] = tagFilterMessage
			continue
		}
		if _, err := store.GetTagByID(ctx, s.db, id); err != nil {
			fields["tag_ids"] = tagFilterMessage
			break
		}
	}
	return store.WebhookFilters{CategoryIDs: cats, TagIDs: tags}.Normalize()
}

func dedupeIDs(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
