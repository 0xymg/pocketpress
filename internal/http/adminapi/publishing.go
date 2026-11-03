package adminapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pocketpress/pocketpress/internal/http/middleware"
	"github.com/pocketpress/pocketpress/internal/store"
)

// publishPost (POST /posts/{id}/publish) transitions a post to published.
func (h *Handler) publishPost(w http.ResponseWriter, r *http.Request) {
	p, err := h.content.PublishPost(r.Context(), r.PathValue("id"))
	if err != nil {
		h.handleContentError(w, err, "Post not found.")
		return
	}
	h.writePostFull(w, http.StatusOK, p)
}

// unpublishPost (POST /posts/{id}/unpublish) moves a post back to draft.
func (h *Handler) unpublishPost(w http.ResponseWriter, r *http.Request) {
	p, err := h.content.UnpublishPost(r.Context(), r.PathValue("id"))
	if err != nil {
		h.handleContentError(w, err, "Post not found.")
		return
	}
	h.writePostFull(w, http.StatusOK, p)
}

// schedulePost (POST /posts/{id}/schedule) schedules a post for a future
// publish time. The body is {"scheduled_for":"<RFC3339>"}; an absent or
// unparseable value is a validation error on scheduled_for, and the service
// rejects a non-future time the same way.
func (h *Handler) schedulePost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ScheduledFor string `json:"scheduled_for"`
	}
	if !h.decode(w, r, &req) {
		return
	}

	when, err := time.Parse(time.RFC3339, strings.TrimSpace(req.ScheduledFor))
	if err != nil {
		h.validationError(w, map[string]string{"scheduled_for": "Scheduled time must be an RFC3339 timestamp."})
		return
	}

	p, err := h.content.SchedulePost(r.Context(), r.PathValue("id"), when)
	if err != nil {
		h.handleContentError(w, err, "Post not found.")
		return
	}
	h.writePostFull(w, http.StatusOK, p)
}

// archivePost (POST /posts/{id}/archive) archives a post.
func (h *Handler) archivePost(w http.ResponseWriter, r *http.Request) {
	p, err := h.content.ArchivePost(r.Context(), r.PathValue("id"))
	if err != nil {
		h.handleContentError(w, err, "Post not found.")
		return
	}
	h.writePostFull(w, http.StatusOK, p)
}

// previewTokenDTO is the frozen preview-token response: the raw token, its
// expiry, and a ready-to-open preview URL (or null when no template is set).
type previewTokenDTO struct {
	Token      string    `json:"token"`
	ExpiresAt  time.Time `json:"expires_at"`
	PreviewURL *string   `json:"preview_url"`
}

// previewToken (POST /posts/{id}/preview-token) mints a short-lived,
// post-scoped preview token and, when settings.preview_url_template is set,
// the resolved preview URL to open it with.
func (h *Handler) previewToken(w http.ResponseWriter, r *http.Request) {
	p, err := h.content.GetPost(r.Context(), r.PathValue("id"))
	if err != nil {
		h.handleContentError(w, err, "Post not found.")
		return
	}

	token, expiresAt := h.preview.Mint(p.Row.ID, time.Now().UTC())

	previewURL, err := h.previewURL(r, token, p.Row.Slug)
	if err != nil {
		h.serverError(w, err)
		return
	}

	middleware.WriteJSON(w, http.StatusOK, previewTokenDTO{
		Token:      token,
		ExpiresAt:  expiresAt,
		PreviewURL: previewURL,
	})
}

// previewURL builds the preview URL from settings.preview_url_template,
// substituting the URL-escaped token for {token} and the slug for {slug}. It
// returns nil (JSON null) when the template is empty or unset.
func (h *Handler) previewURL(r *http.Request, token, slug string) (*string, error) {
	s, err := store.GetSettings(r.Context(), h.db)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(s.PreviewURLTemplate) == "" {
		return nil, nil
	}
	built := strings.ReplaceAll(s.PreviewURLTemplate, "{token}", url.QueryEscape(token))
	built = strings.ReplaceAll(built, "{slug}", slug)
	return &built, nil
}
