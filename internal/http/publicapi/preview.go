package publicapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/pocketpress/pocketpress/internal/apikey"
	"github.com/pocketpress/pocketpress/internal/http/middleware"
	"github.com/pocketpress/pocketpress/internal/preview"
	"github.com/pocketpress/pocketpress/internal/store"
)

// previewPost handles GET /api/v1/preview/posts/{slug} — the authorized
// draft-preview endpoint. Authorization is either:
//   - ?token=… — a short-lived, post-scoped preview token (existing flow), or
//   - Authorization: Bearer ppkey_… with scope preview:read (private API key).
//
// Responses are never cached. Failure modes:
//   - no usable token or API key     -> 401 invalid_preview_token / unauthorized
//   - API key missing preview:read   -> 403 forbidden
//   - no post with that slug         -> 404 post_not_found
//   - token bound to a different post -> 401 invalid_preview_token
func (h *Handler) previewPost(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token != "" {
		h.previewWithToken(w, r, token)
		return
	}

	k, presented, err := h.authenticateAPIKey(r.Context(), r, apikey.ScopePreviewRead)
	if err != nil {
		h.writeAPIKeyAuthError(w, err)
		return
	}
	if !presented {
		h.invalidPreviewToken(w)
		return
	}
	_ = k

	p, err := h.content.GetPostBySlugForPreview(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.postNotFound(w)
			return
		}
		h.serverError(w, err)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"data": toPostDTO(p)})
}

func (h *Handler) previewWithToken(w http.ResponseWriter, r *http.Request, token string) {
	postID, err := h.preview.Verify(token, time.Now().UTC())
	if err != nil {
		if errors.Is(err, preview.ErrInvalidToken) {
			h.invalidPreviewToken(w)
			return
		}
		h.serverError(w, err)
		return
	}

	p, err := h.content.GetPostBySlugForPreview(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.postNotFound(w)
			return
		}
		h.serverError(w, err)
		return
	}

	// A token is bound to one post id; a token for another post must never
	// retrieve this one.
	if p.Row.ID != postID {
		h.invalidPreviewToken(w)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"data": toPostDTO(p)})
}

func (h *Handler) invalidPreviewToken(w http.ResponseWriter) {
	h.writeError(w, http.StatusUnauthorized, "invalid_preview_token", "The preview token is missing, invalid or expired.")
}
