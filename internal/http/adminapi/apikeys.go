package adminapi

import (
	"net/http"
	"time"

	"github.com/pocketpress/pocketpress/internal/apikey"
	"github.com/pocketpress/pocketpress/internal/http/middleware"
	"github.com/pocketpress/pocketpress/internal/store"
)

// apiKeyDTO is the frozen API-key shape. The raw key and token_hash are never
// included (the raw key is returned only by create).
type apiKeyDTO struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func toAPIKeyDTO(k store.APIKey) apiKeyDTO {
	scopes := k.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	return apiKeyDTO{
		ID:         k.ID,
		Name:       k.Name,
		Prefix:     k.Prefix,
		Scopes:     scopes,
		ExpiresAt:  k.ExpiresAt,
		LastUsedAt: k.LastUsedAt,
		RevokedAt:  k.RevokedAt,
		CreatedAt:  k.CreatedAt,
	}
}

// listAPIKeys (GET /api-keys, owner only) returns every API key.
func (h *Handler) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.keys.List(r.Context())
	if err != nil {
		h.serverError(w, err)
		return
	}
	dtos := make([]apiKeyDTO, 0, len(keys))
	for _, k := range keys {
		dtos = append(dtos, toAPIKeyDTO(k))
	}
	middleware.WriteJSON(w, http.StatusOK, map[string][]apiKeyDTO{"api_keys": dtos})
}

// createAPIKey (POST /api-keys, owner only) creates a key and returns it with
// the raw token, shown exactly once.
func (h *Handler) createAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string     `json:"name"`
		Scopes    []string   `json:"scopes"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if !h.decode(w, r, &req) {
		return
	}

	k, raw, err := h.keys.Create(r.Context(), apikey.CreateInput{
		Name:      req.Name,
		Scopes:    req.Scopes,
		ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		h.handleContentError(w, err, "API key not found.")
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]any{
		"api_key": toAPIKeyDTO(k),
		"key":     raw,
	})
}

// revokeAPIKey (POST /api-keys/{id}/revoke, owner only) soft-revokes a key.
func (h *Handler) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	k, err := h.keys.Revoke(r.Context(), r.PathValue("id"))
	if err != nil {
		h.handleContentError(w, err, "API key not found.")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]apiKeyDTO{"api_key": toAPIKeyDTO(k)})
}

// deleteAPIKey (DELETE /api-keys/{id}, owner only) hard-deletes a key row.
func (h *Handler) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	if err := h.keys.Delete(r.Context(), r.PathValue("id")); err != nil {
		h.handleContentError(w, err, "API key not found.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
