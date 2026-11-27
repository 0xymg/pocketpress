// Package adminapi holds the thin HTTP handlers for PocketPress's admin
// API under /api/admin/v1: first-run setup, session login/logout, the
// current-user endpoint, and owner-only user management.
//
// Handlers here only decode and validate the request, call the auth
// service, and serialize the result into the shared JSON envelope. All
// authentication, hashing, session and permission logic lives in
// internal/auth and internal/http/middleware; no domain decision is made in
// this package.
package adminapi

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"time"

	"github.com/pocketpress/pocketpress/internal/apikey"
	"github.com/pocketpress/pocketpress/internal/auth"
	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/http/middleware"
	"github.com/pocketpress/pocketpress/internal/media"
	"github.com/pocketpress/pocketpress/internal/preview"
	"github.com/pocketpress/pocketpress/internal/store"
	"github.com/pocketpress/pocketpress/internal/webhook"
)

// maxBodyBytes caps admin API request bodies. Most admin JSON payloads are
// small; this guards against a client streaming an unbounded body.
const maxBodyBytes = 1 << 20 // 1 MiB

// maxPostBodyBytes caps the post create/update/duplicate request bodies. Posts
// carry the serialized editor state (content_json) and its HTML export
// (content_html), each up to 2 MB, so these endpoints need a larger cap than
// the rest of the admin API.
const maxPostBodyBytes = 5 << 20 // 5 MiB

// minPasswordLen is the minimum accepted password length, per the frozen
// contract.
const minPasswordLen = 10

// Handler serves the admin API. It is constructed once with the auth,
// content and media services and whether cookies should carry the Secure
// attribute (true in production, where PocketPress is served over HTTPS).
type Handler struct {
	svc           *auth.Service
	content       *content.Service
	media         *media.Service
	webhook       *webhook.Service
	keys          *apikey.Service
	db            *sql.DB
	preview       *preview.Tokenizer
	logger        *slog.Logger
	secureCookies bool
}

// New builds an admin API Handler. db backs the settings and preview-token
// endpoints (which read the singleton settings row directly); previewTokens
// mints the short-lived tokens the preview-token endpoint returns; webhookSvc
// and keySvc back the owner-only webhook and API-key management endpoints.
func New(svc *auth.Service, contentSvc *content.Service, mediaSvc *media.Service, webhookSvc *webhook.Service, keySvc *apikey.Service, db *sql.DB, previewTokens *preview.Tokenizer, logger *slog.Logger, secureCookies bool) *Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Handler{svc: svc, content: contentSvc, media: mediaSvc, webhook: webhookSvc, keys: keySvc, db: db, preview: previewTokens, logger: logger, secureCookies: secureCookies}
}

// Register wires every admin API route onto mux. Public routes
// (setup/status, setup, login, logout) are registered directly; the
// current-user route is wrapped in RequireUser; the user-management routes
// are wrapped in RequireUser + RequireOwner.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/v1/setup/status", h.setupStatus)
	mux.HandleFunc("POST /api/admin/v1/setup", h.setup)
	mux.HandleFunc("POST /api/admin/v1/auth/login", h.login)
	mux.HandleFunc("POST /api/admin/v1/auth/logout", h.logout)

	requireUser := middleware.RequireUser(h.svc)
	mux.Handle("GET /api/admin/v1/auth/me", requireUser(http.HandlerFunc(h.me)))
	mux.Handle("PATCH /api/admin/v1/auth/me", requireUser(http.HandlerFunc(h.updateMe)))

	owner := func(next http.HandlerFunc) http.Handler {
		return requireUser(middleware.RequireOwner()(next))
	}
	mux.Handle("GET /api/admin/v1/users", owner(h.listUsers))
	mux.Handle("POST /api/admin/v1/users", owner(h.createUser))
	mux.Handle("PATCH /api/admin/v1/users/{id}", owner(h.updateUser))

	// Content routes: categories, tags, authors and posts. Editors and
	// owners alike may use these (RequireUser only, no RequireOwner).
	user := func(next http.HandlerFunc) http.Handler {
		return requireUser(next)
	}
	mux.Handle("GET /api/admin/v1/authors", user(h.listAuthors))

	mux.Handle("GET /api/admin/v1/categories", user(h.listCategories))
	mux.Handle("POST /api/admin/v1/categories", user(h.createCategory))
	mux.Handle("PATCH /api/admin/v1/categories/{id}", user(h.updateCategory))
	mux.Handle("DELETE /api/admin/v1/categories/{id}", user(h.deleteCategory))

	mux.Handle("GET /api/admin/v1/tags", user(h.listTags))
	mux.Handle("POST /api/admin/v1/tags", user(h.createTag))
	mux.Handle("PATCH /api/admin/v1/tags/{id}", user(h.updateTag))
	mux.Handle("DELETE /api/admin/v1/tags/{id}", user(h.deleteTag))

	mux.Handle("GET /api/admin/v1/posts", user(h.listPosts))
	mux.Handle("POST /api/admin/v1/posts", user(h.createPost))
	// Import routes are static paths and must be registered alongside the
	// {id} routes; ServeMux prefers the literal segment over {id}.
	mux.Handle("POST /api/admin/v1/posts/import/markdown", user(h.importMarkdown))
	mux.Handle("POST /api/admin/v1/posts/import/ghost", user(h.importGhost))
	mux.Handle("POST /api/admin/v1/posts/import/wordpress", user(h.importWordPress))
	mux.Handle("GET /api/admin/v1/posts/{id}", user(h.getPost))
	mux.Handle("PATCH /api/admin/v1/posts/{id}", user(h.updatePost))
	mux.Handle("DELETE /api/admin/v1/posts/{id}", user(h.deletePost))
	mux.Handle("POST /api/admin/v1/posts/{id}/duplicate", user(h.duplicatePost))

	// Publish-state transitions and preview-token minting: available to any
	// authenticated user, same as the rest of the content endpoints.
	mux.Handle("POST /api/admin/v1/posts/{id}/publish", user(h.publishPost))
	mux.Handle("POST /api/admin/v1/posts/{id}/unpublish", user(h.unpublishPost))
	mux.Handle("POST /api/admin/v1/posts/{id}/schedule", user(h.schedulePost))
	mux.Handle("POST /api/admin/v1/posts/{id}/archive", user(h.archivePost))
	mux.Handle("POST /api/admin/v1/posts/{id}/preview-token", user(h.previewToken))

	// Settings: owner-only (editors cannot manage system settings).
	mux.Handle("GET /api/admin/v1/settings", owner(h.getSettings))
	mux.Handle("PATCH /api/admin/v1/settings", owner(h.updateSettings))

	// Webhooks: owner-only (editors cannot manage webhooks, per the plan's
	// permission model). Delivery history and manual retry live here too.
	mux.Handle("GET /api/admin/v1/webhooks", owner(h.listWebhooks))
	mux.Handle("POST /api/admin/v1/webhooks", owner(h.createWebhook))
	mux.Handle("PATCH /api/admin/v1/webhooks/{id}", owner(h.updateWebhook))
	mux.Handle("DELETE /api/admin/v1/webhooks/{id}", owner(h.deleteWebhook))
	mux.Handle("POST /api/admin/v1/webhooks/{id}/rotate-secret", owner(h.rotateWebhookSecret))
	mux.Handle("POST /api/admin/v1/webhooks/{id}/test", owner(h.testWebhook))
	mux.Handle("GET /api/admin/v1/webhooks/{id}/deliveries", owner(h.listWebhookDeliveries))
	mux.Handle("POST /api/admin/v1/deliveries/{id}/retry", owner(h.retryDelivery))

	// API keys: owner-only. Raw key is returned exactly once on create;
	// revoke soft-disables, delete hard-removes.
	mux.Handle("GET /api/admin/v1/api-keys", owner(h.listAPIKeys))
	mux.Handle("POST /api/admin/v1/api-keys", owner(h.createAPIKey))
	mux.Handle("POST /api/admin/v1/api-keys/{id}/revoke", owner(h.revokeAPIKey))
	mux.Handle("DELETE /api/admin/v1/api-keys/{id}", owner(h.deleteAPIKey))

	// Media (Phase 4): upload/list/patch/delete require an authenticated
	// user, same as the rest of the content endpoints.
	mux.Handle("GET /api/admin/v1/media", user(h.listMedia))
	mux.Handle("POST /api/admin/v1/media", user(h.uploadMedia))
	mux.Handle("PATCH /api/admin/v1/media/{id}", user(h.updateMediaAlt))
	mux.Handle("DELETE /api/admin/v1/media/{id}", user(h.deleteMedia))

	// Public file serving: no auth, so a product frontend (or a browser
	// rendering a post's cover image) can load it directly. {name...} is
	// purely cosmetic — serveFile only ever resolves the file by {id} — but
	// keeping it in the path pattern is what makes this route more specific
	// than (and so win over) the "/api/" 404 catch-all registered in
	// internal/http's router.
	mux.HandleFunc("GET /api/files/{id}/{name...}", h.serveFile)
}

// userDTO is the public JSON shape of a user. It deliberately omits
// password_hash (and every other sensitive column) so it can never leak.
type userDTO struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Email       string     `json:"email"`
	Role        string     `json:"role"`
	Slug        string     `json:"slug"`
	Bio         string     `json:"bio"`
	Website     string     `json:"website"`
	Location    string     `json:"location"`
	Avatar      string     `json:"avatar"`
	Active      bool       `json:"active"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func toUserDTO(u store.User) userDTO {
	return userDTO{
		ID:          u.ID,
		Name:        u.Name,
		Email:       u.Email,
		Role:        u.Role,
		Slug:        u.Slug,
		Bio:         u.Bio,
		Website:     u.Website,
		Location:    u.Location,
		Avatar:      u.Avatar,
		Active:      u.Active,
		LastLoginAt: u.LastLoginAt,
		CreatedAt:   u.CreatedAt,
	}
}

// writeUser writes {"user": USER} with the given status.
func (h *Handler) writeUser(w http.ResponseWriter, status int, u store.User) {
	middleware.WriteJSON(w, status, map[string]userDTO{"user": toUserDTO(u)})
}

// decode reads and JSON-decodes the request body into dst under the default
// 1 MiB cap. On a malformed or over-limit body it writes a 400 envelope and
// returns false, so the caller can simply `if !h.decode(...) { return }`.
func (h *Handler) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	return h.decodeLimited(w, r, dst, maxBodyBytes)
}

// decodePost is decode with the larger post body cap (maxPostBodyBytes), used
// by the post create/update endpoints that carry content_json + content_html.
func (h *Handler) decodePost(w http.ResponseWriter, r *http.Request, dst any) bool {
	return h.decodeLimited(w, r, dst, maxPostBodyBytes)
}

// decodeLimited reads and JSON-decodes the request body into dst under the
// given byte cap.
func (h *Handler) decodeLimited(w http.ResponseWriter, r *http.Request, dst any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "Request body is not valid JSON.")
		return false
	}
	return true
}

// validationError writes the frozen 400 validation envelope: code
// "validation_failed" with details mapping field name to a human message.
func (h *Handler) validationError(w http.ResponseWriter, fields map[string]string) {
	middleware.WriteErrorDetails(w, http.StatusBadRequest, "validation_failed", "Validation failed.", fields)
}

// serverError logs the underlying error and writes a generic 500 envelope,
// never exposing the raw error (which may reference internal storage
// details) to the client.
func (h *Handler) serverError(w http.ResponseWriter, err error) {
	h.logger.Error("admin api internal error", slog.String("error", err.Error()))
	middleware.WriteError(w, http.StatusInternalServerError, "internal_error", "An internal error occurred.")
}

// isValidEmail reports whether email is a single, plain addr-spec (no
// display name, no angle brackets).
func isValidEmail(email string) bool {
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email
}

// isValidHTTPURL reports whether raw parses as an absolute http(s) URL with
// a host.
func isValidHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// isValidTimezone reports whether tz names an IANA location loadable via
// time.LoadLocation. Empty is rejected here (callers also check "required"
// first) because LoadLocation("") would otherwise succeed as UTC.
func isValidTimezone(tz string) bool {
	if tz == "" {
		return false
	}
	_, err := time.LoadLocation(tz)
	return err == nil
}

// setSessionCookie sets the session cookie carrying token, with the frozen
// flags: HttpOnly, SameSite=Lax, Path=/, 30-day lifetime, and Secure only
// when configured (production/HTTPS).
func (h *Handler) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().UTC().Add(auth.SessionTTL),
		MaxAge:   int(auth.SessionTTL.Seconds()),
	})
}

// clearSessionCookie expires the session cookie in the client.
func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
