package middleware

import (
	"net/http"
	"strings"
)

// SecurityConfig configures the SecurityHeaders middleware.
type SecurityConfig struct {
	// SPACSP is the Content-Security-Policy applied to the admin SPA (every
	// non-/api path). It is computed once at startup from the embedded admin
	// assets so it stays in sync with any inline bootstrap script the build
	// emits (see internal/http's SPA CSP builder). When empty, no CSP header
	// is set.
	SPACSP string

	// Production is true when PP_ENV=production. It is the precondition for the
	// HSTS header — PocketPress never asserts Strict-Transport-Security outside
	// production, where an accidental HTTPS visit could otherwise pin a browser
	// to HTTPS against a dev instance served over plain HTTP.
	Production bool

	// PublicURLHTTPS is true when PP_PUBLIC_URL is an https URL. Together with a
	// direct TLS connection it is what lets HSTS be asserted for the common
	// deployment where TLS is terminated at a reverse proxy and PocketPress
	// itself sees plain HTTP.
	PublicURLHTTPS bool
}

// hstsValue is the Strict-Transport-Security value: one year, per the
// development plan's hardening slice. Subdomains and preload are intentionally
// omitted — PocketPress is a single host and does not speak for its parent
// domain.
const hstsValue = "max-age=31536000"

// SecurityHeaders returns middleware that attaches PocketPress's security
// response headers.
//
// On every response it sets:
//   - X-Content-Type-Options: nosniff
//   - X-Frame-Options: DENY
//   - Referrer-Policy: strict-origin-when-cross-origin
//   - Strict-Transport-Security (only in production over TLS, or behind an
//     https PP_PUBLIC_URL — the terminated-at-proxy case)
//
// On admin SPA responses only (every path that is not under /api/) it also
// sets the configured Content-Security-Policy. API (JSON) responses never get
// a CSP — a CSP is a document-security control and would only be noise on JSON.
//
// Headers are set before the wrapped handler runs, so they are present even on
// responses the inner handlers short-circuit (rate-limit 429s, CORS preflight
// 204s) and on panic-recovery 500s.
func SecurityHeaders(cfg SecurityConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

			if cfg.Production && (r.TLS != nil || cfg.PublicURLHTTPS) {
				h.Set("Strict-Transport-Security", hstsValue)
			}

			if cfg.SPACSP != "" && !strings.HasPrefix(r.URL.Path, "/api/") {
				h.Set("Content-Security-Policy", cfg.SPACSP)
			}

			next.ServeHTTP(w, r)
		})
	}
}
