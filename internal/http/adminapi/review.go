package adminapi

import (
	"net/http"

	"github.com/pocketpress/pocketpress/internal/http/middleware"
)

// submitReview (POST /posts/{id}/submit-review) moves a draft/archived post
// into pending review.
func (h *Handler) submitReview(w http.ResponseWriter, r *http.Request) {
	p, err := h.content.SubmitForReview(r.Context(), r.PathValue("id"))
	if err != nil {
		h.handleContentError(w, err, "Post not found.")
		return
	}
	h.writePostFull(w, http.StatusOK, p)
}

// approveReview (POST /posts/{id}/approve-review) approves a pending post.
func (h *Handler) approveReview(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		middleware.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
		return
	}
	p, err := h.content.ApproveReview(r.Context(), r.PathValue("id"), user.ID)
	if err != nil {
		h.handleContentError(w, err, "Post not found.")
		return
	}
	h.writePostFull(w, http.StatusOK, p)
}

// requestChanges (POST /posts/{id}/request-changes) requests changes on a
// pending post. Body: {"note":"..."}.
func (h *Handler) requestChanges(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		middleware.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	if !h.decode(w, r, &req) {
		return
	}
	p, err := h.content.RequestChanges(r.Context(), r.PathValue("id"), user.ID, req.Note)
	if err != nil {
		h.handleContentError(w, err, "Post not found.")
		return
	}
	h.writePostFull(w, http.StatusOK, p)
}

// withdrawReview (POST /posts/{id}/withdraw-review) returns a pending post
// to review_status none.
func (h *Handler) withdrawReview(w http.ResponseWriter, r *http.Request) {
	p, err := h.content.WithdrawReview(r.Context(), r.PathValue("id"))
	if err != nil {
		h.handleContentError(w, err, "Post not found.")
		return
	}
	h.writePostFull(w, http.StatusOK, p)
}
