// Package publicapi implements PocketPress's public, unauthenticated,
// versioned content API under /api/v1: posts, categories, tags,
// syndication feeds (RSS/Atom), and a Sitemap XML document.
//
// Every route here only ever surfaces publicly-visible content — see the
// hard visibility rule enforced by content.Service's ListPublicPosts and
// GetPublicPostBySlug (status = published, published_at set and not in the
// future) — and never leaks admin-only fields (status, content_json,
// content_text, scheduled_for, internal timestamps, user emails, ...).
// Serializers live in serializers.go and are built fresh for this package;
// they deliberately do not reuse the admin API's DTOs (internal/http/
// adminapi), so the two contracts can evolve independently.
//
// Handlers here stay thin: query-param parsing and response shaping only.
// Visibility rules, filtering and pagination all live in content.Service
// and internal/store's PostFilter.
package publicapi

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"github.com/pocketpress/pocketpress/internal/apikey"
	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/http/middleware"
	"github.com/pocketpress/pocketpress/internal/preview"
	"github.com/pocketpress/pocketpress/internal/store"
)

// listCacheControl is the Cache-Control value for every endpoint except
// the post detail endpoint (which has its own ETag-driven caching; see
// posts.go). This covers the posts list, category/tag-scoped post lists,
// and the categories/tags list and detail endpoints.
const listCacheControl = "public, max-age=60"

// Handler serves the public content API. It is constructed once with the
// content service and the open database handle (needed directly for the
// settings-driven public_api_enabled gate, which has no other home in the
// content service).
type Handler struct {
	content *content.Service
	keys    *apikey.Service
	db      *sql.DB
	preview *preview.Tokenizer
	logger  *slog.Logger
}

// New builds a public API Handler. previewTokens verifies the tokens the
// preview endpoint requires. keys authorizes Bearer API keys for private
// content access when public_api_enabled is false (and for preview:read).
func New(contentSvc *content.Service, keySvc *apikey.Service, db *sql.DB, previewTokens *preview.Tokenizer, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Handler{content: contentSvc, keys: keySvc, db: db, preview: previewTokens, logger: logger}
}

// Register wires every public API route onto mux, each behind the
// public_api_enabled gate (see gate below).
func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/posts", h.gate(h.listPosts))
	mux.Handle("GET /api/v1/posts/{slug}", h.gate(h.getPost))

	mux.Handle("GET /api/v1/categories", h.gate(h.listCategories))
	mux.Handle("GET /api/v1/categories/{slug}", h.gate(h.getCategory))
	mux.Handle("GET /api/v1/categories/{slug}/posts", h.gate(h.categoryPosts))

	mux.Handle("GET /api/v1/tags", h.gate(h.listTags))
	mux.Handle("GET /api/v1/tags/{slug}", h.gate(h.getTag))
	mux.Handle("GET /api/v1/tags/{slug}/posts", h.gate(h.tagPosts))

	mux.Handle("GET /api/v1/authors", h.gate(h.listAuthors))
	mux.Handle("GET /api/v1/authors/{slug}", h.gate(h.getAuthor))
	mux.Handle("GET /api/v1/authors/{slug}/posts", h.gate(h.authorPosts))

	// Syndication feeds (v0.2): newest published posts as RSS 2.0 / Atom 1.0.
	mux.Handle("GET /api/v1/feed.rss", h.gate(h.feedRSS))
	mux.Handle("GET /api/v1/feed.atom", h.gate(h.feedAtom))

	// Sitemap (v0.2): all publicly-visible post permalinks as Sitemap 0.9 XML.
	mux.Handle("GET /api/v1/sitemap.xml", h.gate(h.sitemap))

	// The preview endpoint is deliberately NOT behind the public_api_enabled
	// gate: it is an authorized flow (a valid, post-scoped preview token, or
	// a Bearer API key with preview:read), so it must keep working for a
	// product frontend previewing drafts even when the public content API
	// is switched off.
	mux.HandleFunc("GET /api/v1/preview/posts/{slug}", h.previewPost)
}

// gate wraps next so every /api/v1/* content route first checks
// settings.public_api_enabled. When the public API is disabled, a Bearer
// API key with content:read may still access the route (private content
// API). Without a usable key the frozen 404 "api_disabled" envelope is
// returned (including before first-run setup — there is no content to
// serve either way).
func (h *Handler) gate(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enabled, err := h.publicAPIEnabled(r.Context())
		if err != nil {
			h.serverError(w, err)
			return
		}
		if enabled {
			// Optional API key: if a ppkey_ Bearer is presented, validate it
			// (bad keys fail closed); otherwise serve publicly as before.
			if k, presented, err := h.authenticateAPIKey(r.Context(), r, apikey.ScopeContentRead); presented {
				if err != nil {
					h.writeAPIKeyAuthError(w, err)
					return
				}
				r = r.WithContext(withAPIKey(r.Context(), k))
			}
			next.ServeHTTP(w, r)
			return
		}

		k, presented, err := h.authenticateAPIKey(r.Context(), r, apikey.ScopeContentRead)
		if err != nil {
			h.writeAPIKeyAuthError(w, err)
			return
		}
		if !presented {
			h.writeError(w, http.StatusNotFound, "api_disabled", "The public API is disabled.")
			return
		}
		ctx := withPrivateAuth(withAPIKey(r.Context(), k))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handler) writeAPIKeyAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, apikey.ErrMissingScope):
		h.writeError(w, http.StatusForbidden, "forbidden", "The API key does not have the required scope.")
	case errors.Is(err, apikey.ErrInvalidKey):
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "The API key is missing, invalid, expired or revoked.")
	default:
		h.serverError(w, err)
	}
}

func (h *Handler) publicAPIEnabled(ctx context.Context) (bool, error) {
	s, err := store.GetSettings(ctx, h.db)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return s.PublicAPIEnabled, nil
}

// SettingsCORSOrigins builds a middleware.AllowedOriginsFunc that reads the
// current settings.cors_origins allowlist from db on every call. A missing
// settings row (before first-run setup) or any other lookup error yields
// no dynamic origins rather than failing the request — the static
// PP_CORS_ORIGINS allowlist (see internal/app.Config.CORSOrigins) still
// applies on its own in that case.
func SettingsCORSOrigins(db *sql.DB) middleware.AllowedOriginsFunc {
	return func(r *http.Request) []string {
		s, err := store.GetSettings(r.Context(), db)
		if err != nil {
			return nil
		}
		return s.CORSOrigins
	}
}

// ---------------------------------------------------------------------------
// Response helpers
// ---------------------------------------------------------------------------

// writeData writes {"data": v} with the given status, Cache-Control and the
// X-Content-Type-Options: nosniff header every public API response carries.
// Private API-key access forces Cache-Control: private, no-store.
func (h *Handler) writeData(w http.ResponseWriter, r *http.Request, status int, cacheControl string, v any) {
	w.Header().Set("Cache-Control", resolveCacheControl(r.Context(), cacheControl))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	middleware.WriteJSON(w, status, map[string]any{"data": v})
}

// writeListData writes {"data": v, "meta": meta} with the given status and
// Cache-Control.
func (h *Handler) writeListData(w http.ResponseWriter, r *http.Request, status int, cacheControl string, v any, meta metaDTO) {
	w.Header().Set("Cache-Control", resolveCacheControl(r.Context(), cacheControl))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	middleware.WriteJSON(w, status, map[string]any{"data": v, "meta": meta})
}

// writeError writes PocketPress's standard error envelope, with the
// X-Content-Type-Options: nosniff header every public API response
// carries.
func (h *Handler) writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	middleware.WriteError(w, status, code, message)
}

func (h *Handler) postNotFound(w http.ResponseWriter) {
	h.writeError(w, http.StatusNotFound, "post_not_found", "The requested post was not found.")
}

func (h *Handler) categoryNotFound(w http.ResponseWriter) {
	h.writeError(w, http.StatusNotFound, "category_not_found", "The requested category was not found.")
}

func (h *Handler) tagNotFound(w http.ResponseWriter) {
	h.writeError(w, http.StatusNotFound, "tag_not_found", "The requested tag was not found.")
}

func (h *Handler) authorNotFound(w http.ResponseWriter) {
	h.writeError(w, http.StatusNotFound, "author_not_found", "The requested author was not found.")
}

// serverError logs the underlying error and writes a generic 500 envelope,
// never exposing the raw error (which may reference internal storage
// details) to the client.
func (h *Handler) serverError(w http.ResponseWriter, err error) {
	h.logger.Error("public api internal error", slog.String("error", err.Error()))
	h.writeError(w, http.StatusInternalServerError, "internal_error", "An internal error occurred.")
}
