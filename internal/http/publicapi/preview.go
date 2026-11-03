package publicapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/pocketpress/pocketpress/internal/http/middleware"
	"github.com/pocketpress/pocketpress/internal/preview"
	"github.com/pocketpress/pocketpress/internal/store"
)

// previewPost handles GET /api/v1/preview/posts/{slug}?token=... — the
// authorized draft-preview endpoint. It verifies the token, loads the post by
// slug regardless of status, and confirms the token was minted for that exact
// post before serving it with the same public serializer as the published
// detail endpoint. Responses are never cached.
//
// Failure modes, checked in this order:
//   - missing/invalid/expired token  -> 401 invalid_preview_token
//   - no post with that slug         -> 404 post_not_found
//   - token bound to a different post -> 401 invalid_preview_token
func (h *Handler) previewPost(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		h.invalidPreviewToken(w)
		return
	}

	postID, err := h.preview.Verify(token, time.Now().UTC())
	if err != nil {
		// preview.ErrInvalidToken (or any verify failure) is an authorization
		// failure, not a server error.
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
