package adminapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/pocketpress/pocketpress/internal/http/middleware"
	"github.com/pocketpress/pocketpress/internal/store"
	"github.com/pocketpress/pocketpress/internal/webhook"
)

// Pagination bounds for the delivery-history list (frozen contract: default 20,
// max 100).
const (
	deliveriesDefaultPerPage = 20
	deliveriesMaxPerPage     = 100
)

// webhookFiltersDTO is the additive filters object on a webhook (v0.2).
type webhookFiltersDTO struct {
	CategoryIDs []string `json:"category_ids"`
	TagIDs      []string `json:"tag_ids"`
}

// webhookDTO is the WH shape. The signing secret is never included here
// (it is returned only by create and rotate-secret). filters is additive (v0.2).
type webhookDTO struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	URL       string            `json:"url"`
	Events    []string          `json:"events"`
	Filters   webhookFiltersDTO `json:"filters"`
	Active    bool              `json:"active"`
	CreatedAt time.Time         `json:"created_at"`
}

func toWebhookDTO(w store.Webhook) webhookDTO {
	events := w.Events
	if events == nil {
		events = []string{}
	}
	f := w.Filters.Normalize()
	return webhookDTO{
		ID:     w.ID,
		Name:   w.Name,
		URL:    w.URL,
		Events: events,
		Filters: webhookFiltersDTO{
			CategoryIDs: f.CategoryIDs,
			TagIDs:      f.TagIDs,
		},
		Active:    w.Active,
		CreatedAt: w.CreatedAt,
	}
}

// deliveryDTO is the frozen DEL shape.
type deliveryDTO struct {
	ID            string     `json:"id"`
	Event         string     `json:"event"`
	EventID       string     `json:"event_id"`
	Status        string     `json:"status"`
	Attempt       int        `json:"attempt"`
	ResponseCode  *int       `json:"response_code"`
	ResponseBody  *string    `json:"response_body"`
	NextAttemptAt *time.Time `json:"next_attempt_at"`
	DeliveredAt   *time.Time `json:"delivered_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

func toDeliveryDTO(d store.Delivery) deliveryDTO {
	return deliveryDTO{
		ID:            d.ID,
		Event:         d.Event,
		EventID:       d.EventID,
		Status:        d.Status,
		Attempt:       d.Attempt,
		ResponseCode:  d.ResponseCode,
		ResponseBody:  d.ResponseBody,
		NextAttemptAt: d.NextAttemptAt,
		DeliveredAt:   d.DeliveredAt,
		CreatedAt:     d.CreatedAt,
	}
}

// listWebhooks (GET /webhooks, owner only) returns every webhook.
func (h *Handler) listWebhooks(w http.ResponseWriter, r *http.Request) {
	webhooks, err := h.webhook.List(r.Context())
	if err != nil {
		h.serverError(w, err)
		return
	}
	dtos := make([]webhookDTO, 0, len(webhooks))
	for _, wh := range webhooks {
		dtos = append(dtos, toWebhookDTO(wh))
	}
	middleware.WriteJSON(w, http.StatusOK, map[string][]webhookDTO{"webhooks": dtos})
}

// createWebhook (POST /webhooks, owner only) creates a webhook and returns it
// with its signing secret, shown exactly once. active defaults to true when the
// field is absent.
func (h *Handler) createWebhook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string                `json:"name"`
		URL     string                `json:"url"`
		Events  []string              `json:"events"`
		Filters *store.WebhookFilters `json:"filters"`
		Active  *bool                 `json:"active"`
	}
	if !h.decode(w, r, &req) {
		return
	}

	active := true
	if req.Active != nil {
		active = *req.Active
	}
	filters := store.WebhookFilters{}
	if req.Filters != nil {
		filters = *req.Filters
	}

	wh, secret, err := h.webhook.Create(r.Context(), webhook.CreateInput{
		Name:    req.Name,
		URL:     req.URL,
		Events:  req.Events,
		Filters: filters,
		Active:  active,
	})
	if err != nil {
		h.handleContentError(w, err, "Webhook not found.")
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]any{
		"webhook": toWebhookDTO(wh),
		"secret":  secret,
	})
}

// updateWebhook (PATCH /webhooks/{id}, owner only) applies a partial update.
func (h *Handler) updateWebhook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    *string               `json:"name"`
		URL     *string               `json:"url"`
		Events  *[]string             `json:"events"`
		Filters *store.WebhookFilters `json:"filters"`
		Active  *bool                 `json:"active"`
	}
	if !h.decode(w, r, &req) {
		return
	}

	wh, err := h.webhook.Update(r.Context(), r.PathValue("id"), webhook.UpdatePatch{
		Name:    req.Name,
		URL:     req.URL,
		Events:  req.Events,
		Filters: req.Filters,
		Active:  req.Active,
	})
	if err != nil {
		h.handleContentError(w, err, "Webhook not found.")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]webhookDTO{"webhook": toWebhookDTO(wh)})
}

// deleteWebhook (DELETE /webhooks/{id}, owner only) removes a webhook; its
// deliveries cascade away.
func (h *Handler) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	if err := h.webhook.Delete(r.Context(), r.PathValue("id")); err != nil {
		h.handleContentError(w, err, "Webhook not found.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// rotateWebhookSecret (POST /webhooks/{id}/rotate-secret, owner only) issues a
// new signing secret and returns it once.
func (h *Handler) rotateWebhookSecret(w http.ResponseWriter, r *http.Request) {
	secret, err := h.webhook.RotateSecret(r.Context(), r.PathValue("id"))
	if err != nil {
		h.handleContentError(w, err, "Webhook not found.")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"secret": secret})
}

// testWebhook (POST /webhooks/{id}/test, owner only) enqueues a webhook.test
// delivery, regardless of the webhook's event subscription, and returns the
// new delivery id.
func (h *Handler) testWebhook(w http.ResponseWriter, r *http.Request) {
	deliveryID, err := h.webhook.SendTest(r.Context(), r.PathValue("id"))
	if err != nil {
		h.handleContentError(w, err, "Webhook not found.")
		return
	}
	middleware.WriteJSON(w, http.StatusAccepted, map[string]string{"delivery_id": deliveryID})
}

// listWebhookDeliveries (GET /webhooks/{id}/deliveries, owner only) returns a
// paginated, newest-first page of a webhook's delivery history.
func (h *Handler) listWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := parsePositiveInt(q.Get("page"), 1)
	perPage := parsePositiveInt(q.Get("per_page"), deliveriesDefaultPerPage)
	if perPage > deliveriesMaxPerPage {
		perPage = deliveriesMaxPerPage
	}

	result, err := h.webhook.ListDeliveries(r.Context(), r.PathValue("id"), page, perPage)
	if err != nil {
		h.handleContentError(w, err, "Webhook not found.")
		return
	}

	dtos := make([]deliveryDTO, 0, len(result.Deliveries))
	for _, d := range result.Deliveries {
		dtos = append(dtos, toDeliveryDTO(d))
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{
		"deliveries": dtos,
		"meta": map[string]int{
			"page":        result.Page,
			"per_page":    result.PerPage,
			"total_items": result.TotalItems,
			"total_pages": result.TotalPages,
		},
	})
}

// retryDelivery (POST /deliveries/{id}/retry, owner only) re-queues a failed
// delivery for one more attempt. A delivery that is not failed yields a 400.
func (h *Handler) retryDelivery(w http.ResponseWriter, r *http.Request) {
	err := h.webhook.RetryDelivery(r.Context(), r.PathValue("id"))
	switch {
	case errors.Is(err, webhook.ErrDeliveryNotFailed):
		middleware.WriteError(w, http.StatusBadRequest, "invalid_state", "Only a failed delivery can be retried.")
		return
	case errors.Is(err, store.ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not_found", "Delivery not found.")
		return
	case err != nil:
		h.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
