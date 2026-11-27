// Package http builds PocketPress's top-level http.Handler: the /healthz
// probe, the /api/admin/v1 admin API, the /api/v1 public content API, the
// reserved /api/ namespace, and the embedded admin SPA — wrapped in the
// public API's CORS middleware, request logging and panic recovery
// middleware.
package http

import (
	"database/sql"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/pocketpress/pocketpress/internal/auth"
	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/http/adminapi"
	"github.com/pocketpress/pocketpress/internal/http/middleware"
	"github.com/pocketpress/pocketpress/internal/http/publicapi"
	"github.com/pocketpress/pocketpress/internal/media"
	"github.com/pocketpress/pocketpress/internal/preview"
	"github.com/pocketpress/pocketpress/internal/webhook"
)

// Config holds the dependencies NewRouter needs to build the handler.
type Config struct {
	// Logger is the structured logger threaded through middleware and
	// handlers.
	Logger *slog.Logger

	// Version is the PocketPress version reported by /healthz.
	Version string

	// DB is the open SQLite handle the admin API's auth service uses. It is
	// created by app.Serve after migrations run, then passed here.
	DB *sql.DB

	// SecureCookies marks session cookies Secure (HTTPS-only). It is true
	// in production.
	SecureCookies bool

	// MediaDriver is the storage backend (local disk or S3-compatible) the
	// media service saves/opens/deletes uploaded files through. Built from
	// PP_STORAGE_DRIVER and friends before the server starts (see
	// cmd/pocketpress), so an S3 misconfiguration fails fast at startup
	// rather than on the first upload.
	MediaDriver media.Driver

	// DataDir is PP_DATA_DIR. The media service roots its on-demand
	// transform cache at <DataDir>/transforms.
	DataDir string

	// MaxUploadMB caps a single media upload's size, in megabytes.
	MaxUploadMB int

	// CORSOrigins is the static public API CORS allowlist (PP_CORS_ORIGINS).
	// It is merged with the runtime settings.cors_origins allowlist by the
	// CORS middleware guarding /api/v1/* and /api/files/*; see
	// internal/http/publicapi.SettingsCORSOrigins.
	CORSOrigins []string

	// AppSecret is the resolved HMAC secret for preview tokens (from
	// PP_APP_SECRET, or an ephemeral random value generated at startup — see
	// app.ResolvePreviewSecret). It signs and verifies the short-lived,
	// post-scoped tokens the preview flow uses.
	AppSecret []byte

	// EncryptionKey is the resolved 32-byte AES-256 key that encrypts webhook
	// signing secrets at rest (see app.ResolveEncryptionKey). The zero value is
	// a valid AES-256 key, so a minimal Config (e.g. in a healthz/SPA-only
	// test) still builds without panicking; it only matters once a webhook is
	// created.
	EncryptionKey [32]byte

	// Production toggles the strict webhook URL validation (https required plus
	// the SSRF host allowlist) and installs the delivery worker's SSRF dial
	// guard. It is true when PP_ENV=production. It also gates the HSTS response
	// header (see the security-headers middleware).
	Production bool

	// PublicURL is the externally reachable base URL (PP_PUBLIC_URL), if any.
	// It is used only to decide whether HSTS should be asserted when TLS is
	// terminated at a reverse proxy (PocketPress then sees plain HTTP, but an
	// https PublicURL signals the edge is HTTPS).
	PublicURL string

	// TrustedProxies is the reverse-proxy allowlist (PP_TRUSTED_PROXIES) whose
	// X-Forwarded-For header the login rate limiter honors when resolving the
	// client IP. Empty means X-Forwarded-For is never trusted.
	TrustedProxies []string
}

// Server bundles the built HTTP handler with the shared runtime services the
// process supervises alongside it: the single content service (so the
// scheduled-publish worker fires webhook events through the same service the
// request handlers use) and the webhook delivery worker. cmd maps this to
// app.Components.
type Server struct {
	Handler http.Handler
	Content *content.Service
	Worker  *webhook.Worker
}

// NewRouter builds the complete PocketPress HTTP handler.
//
// Routes:
//   - GET /healthz        - fixed liveness payload {"status":"ok","version":"..."}.
//   - /api/admin/v1/*     - the admin API: first-run setup, session auth,
//     current user, owner-only user management, and the content endpoints —
//     categories, tags, authors, posts and media (see internal/http/adminapi).
//   - GET /api/files/{id}/{name...} - public (no auth) media file serving,
//     registered by adminapi.Register alongside the media endpoints above.
//   - /api/v1/*           - the public, unauthenticated content API: posts,
//     categories and tags (see internal/http/publicapi). Gated by
//     settings.public_api_enabled.
//   - /api/*              - any other /api path returns a 404 JSON envelope
//     rather than falling through to the SPA.
//   - /                    - the embedded admin SPA, serving static files with
//     their natural content types and falling back to index.html for any
//     other GET path so client-side routes (e.g. /posts/new) resolve.
//
// The whole handler is wrapped in the public API's CORS middleware (scoped
// to /api/v1/* and /api/files/* only; every other route is untouched by
// it), then request-logging and panic-recovery middleware.
func NewRouter(cfg Config) http.Handler {
	return Build(cfg).Handler
}

// Build assembles the HTTP handler and the shared runtime services (see
// Server). It is the composition root for the request-handling services: it
// constructs the content service exactly once, installs the webhook dispatcher
// as its event sink (so publish/unpublish/update/delete — from HTTP or the
// scheduler — enqueue deliveries), and builds the webhook delivery worker the
// caller runs for the server's lifetime.
func Build(cfg Config) *Server {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", healthzHandler(cfg.Version))

	authService := auth.NewService(cfg.DB, cfg.Logger)
	contentService := content.NewService(cfg.DB, cfg.Logger)
	mediaService := media.NewService(cfg.DB, cfg.MediaDriver, cfg.MaxUploadMB, cfg.DataDir, cfg.Logger)
	previewTokens := preview.New(cfg.AppSecret)

	// Webhook subsystem: the dispatcher becomes the content service's event
	// sink, so every publish-state change fans out to durable delivery rows the
	// worker drains asynchronously.
	cipher := webhook.NewCipher(cfg.EncryptionKey)
	dispatcher := webhook.NewDispatcher(cfg.DB, cfg.Logger)
	contentService.SetEvents(dispatcher)
	webhookService := webhook.NewService(cfg.DB, cipher, dispatcher, cfg.Production, cfg.Logger)
	worker := webhook.NewWorker(cfg.DB, cipher, webhook.NewDeliveryClient(cfg.Production), cfg.Logger)

	adminapi.New(authService, contentService, mediaService, webhookService, cfg.DB, previewTokens, cfg.Logger, cfg.SecureCookies).Register(mux)

	// The public API is registered before the /api/ 404 catch-all below so
	// its explicit /api/v1/* routes take precedence over it.
	publicapi.New(contentService, cfg.DB, previewTokens, cfg.Logger).Register(mux)

	// Any /api path not matched by a more specific pattern above returns
	// the standard 404 envelope instead of falling through to the SPA.
	mux.HandleFunc("/api/", apiNotFoundHandler)

	spaHandler, err := newSPAHandler()
	if err != nil {
		cfg.Logger.Error("failed to load embedded admin assets", slog.String("error", err.Error()))
		spaHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			middleware.WriteError(w, http.StatusInternalServerError, "internal_error", "Admin assets unavailable.")
		})
	}
	mux.Handle("/", spaHandler)

	corsOrigins := publicCORSOrigins(cfg.CORSOrigins, publicapi.SettingsCORSOrigins(cfg.DB))

	// Login/setup rate limiter and the trusted-proxy allowlist that governs
	// how the client IP is resolved behind a reverse proxy.
	loginLimiter := middleware.NewRateLimiter(loginRateLimit, loginRateWindow)
	trustedProxies := middleware.ParseTrustedProxies(cfg.TrustedProxies)

	securityCfg := middleware.SecurityConfig{
		SPACSP:         buildSPACSP(),
		Production:     cfg.Production,
		PublicURLHTTPS: isHTTPSURL(cfg.PublicURL),
	}

	// Middleware chain (outermost first): request logging, panic recovery,
	// security headers (so even short-circuited 429/preflight/500 responses
	// carry them), login rate limiting, the global body-size backstop, then
	// the public API CORS scoping, wrapping the route mux.
	var handler http.Handler = mux
	handler = middleware.CORS(corsOrigins)(handler)
	handler = middleware.BodyLimit(bodyLimitFor(cfg.MaxUploadMB))(handler)
	handler = middleware.LoginRateLimit(loginLimiter, trustedProxies)(handler)
	handler = middleware.SecurityHeaders(securityCfg)(handler)
	handler = middleware.Recovery(cfg.Logger)(handler)
	handler = middleware.RequestLogger(cfg.Logger)(handler)

	return &Server{Handler: handler, Content: contentService, Worker: worker}
}

// Login rate-limit policy: at most loginRateLimit attempts per client IP within
// any trailing loginRateWindow, on POST /auth/login and POST /setup.
const (
	loginRateLimit  = 5
	loginRateWindow = time.Minute
)

// Body-size caps enforced by the global BodyLimit backstop. Most routes get
// defaultBodyLimit; the post write endpoints and media upload get more, so the
// backstop never trips before the handlers' own (equal-or-tighter) caps.
const (
	postBodyLimit  = 5 << 20 // matches adminapi's post decode cap
	mediaBodySlack = 1 << 20 // headroom over the media handler's own upload cap
)

// bodyLimitFor returns the per-request body cap for the global BodyLimit
// middleware. Post write paths are granted the 5 MiB post cap; the media upload
// endpoint is granted the configured upload cap plus slack (so the media
// handler's own MaxBytesReader — which yields the precise validation envelope —
// is what actually rejects an oversized upload); everything else gets the 1 MiB
// default.
func bodyLimitFor(maxUploadMB int) func(*http.Request) int64 {
	if maxUploadMB <= 0 {
		maxUploadMB = 10
	}
	mediaLimit := int64(maxUploadMB)*(1<<20) + mediaBodySlack
	return func(r *http.Request) int64 {
		p := r.URL.Path
		if strings.HasPrefix(p, "/api/admin/v1/posts") {
			return postBodyLimit
		}
		if r.Method == http.MethodPost && p == "/api/admin/v1/media" {
			return mediaLimit
		}
		return middleware.DefaultMaxBodyBytes
	}
}

// isHTTPSURL reports whether raw is an https URL (used to decide HSTS behind a
// TLS-terminating proxy).
func isHTTPSURL(raw string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "https://")
}

// publicCORSOrigins merges the static PP_CORS_ORIGINS allowlist with the
// runtime settings.cors_origins allowlist (dynamicOrigins), resolved fresh
// on every request.
func publicCORSOrigins(staticOrigins []string, dynamicOrigins middleware.AllowedOriginsFunc) middleware.AllowedOriginsFunc {
	return func(r *http.Request) []string {
		merged := make([]string, 0, len(staticOrigins))
		merged = append(merged, staticOrigins...)
		merged = append(merged, dynamicOrigins(r)...)
		return merged
	}
}

// healthzHandler returns the fixed health payload:
// {"status":"ok","version":"<version>"}.
func healthzHandler(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		middleware.WriteJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"version": version,
		})
	}
}

// apiNotFoundHandler answers every /api/* path not otherwise registered
// with PocketPress's standard 404 JSON envelope, since the versioned API
// doesn't exist yet (Phase 1) and must never fall through to the SPA.
func apiNotFoundHandler(w http.ResponseWriter, r *http.Request) {
	middleware.WriteError(w, http.StatusNotFound, "not_found", "Not found.")
}
