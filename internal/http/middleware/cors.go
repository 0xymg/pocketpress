package middleware

import (
	"net/http"
	"strings"
)

// publicCORSPrefixes are the only path prefixes the CORS middleware acts
// on: the public content API and public file serving. Every other path
// (admin API, embedded SPA) is left completely untouched — in particular,
// the cookie-authenticated admin API never gets an
// Access-Control-Allow-Origin header from this middleware.
var publicCORSPrefixes = []string{"/api/v1/", "/api/files/"}

// AllowedOriginsFunc resolves the current CORS allowlist for a request.
// Implementations may merge a static configuration allowlist with a
// runtime one (e.g. a settings table); see internal/http/publicapi's
// SettingsCORSOrigins helper for the latter.
type AllowedOriginsFunc func(r *http.Request) []string

// CORS builds PocketPress's public API CORS middleware, per the
// development plan's CORS requirements (section 10) and Phase 5's
// allowlist behavior:
//
//   - Scope: only requests to /api/v1/* and /api/files/* are considered;
//     every other request passes through completely unmodified (no headers
//     added, no OPTIONS short-circuit).
//   - Exact-match allowlist: allowedOrigins(r) is consulted fresh per
//     request; an Origin header that exactly matches one of its entries is
//     "allowed".
//   - Allowed origin: sets Access-Control-Allow-Origin: <origin>,
//     Vary: Origin and Access-Control-Allow-Methods: GET, HEAD, OPTIONS.
//   - No matching origin (including no Origin header at all, i.e. a
//     same-origin or non-browser request): no CORS headers are added at
//     all, and the request is still served normally — this middleware
//     never blocks a request server-side, since enforcement happens in the
//     requesting browser based on the headers present.
//   - Never emits Access-Control-Allow-Credentials: the public API is
//     cookie-free by design, so credentialed CORS is never in play here.
//   - OPTIONS preflight requests under the public scope always get a bare
//     204, short-circuiting before the wrapped handler — with the same
//     allowed-origin headers applied first when the origin matches.
func CORS(allowedOrigins AllowedOriginsFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isPublicCORSPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			origin := r.Header.Get("Origin")
			if origin != "" && originAllowed(origin, allowedOrigins(r)) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// isPublicCORSPath reports whether path falls under the public API's CORS
// scope.
func isPublicCORSPath(path string) bool {
	for _, prefix := range publicCORSPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// originAllowed reports whether origin exactly matches one of allowed.
func originAllowed(origin string, allowed []string) bool {
	for _, a := range allowed {
		if a == origin {
			return true
		}
	}
	return false
}
