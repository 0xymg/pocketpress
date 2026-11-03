package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func nextOK() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func staticOrigins(origins ...string) AllowedOriginsFunc {
	return func(r *http.Request) []string { return origins }
}

// TestCORSAllowedOriginGetsHeaders confirms a matching Origin on an
// in-scope path gets the three CORS response headers, and the request
// still reaches the wrapped handler.
func TestCORSAllowedOriginGetsHeaders(t *testing.T) {
	handler := CORS(staticOrigins("https://example.com"))(nextOK())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (request should still reach next)", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "https://example.com")
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("Vary = %q, want %q", got, "Origin")
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, HEAD, OPTIONS" {
		t.Fatalf("Access-Control-Allow-Methods = %q, want %q", got, "GET, HEAD, OPTIONS")
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want unset (public API is cookie-free)", got)
	}
}

// TestCORSDisallowedOriginGetsNoHeadersButStillServed confirms a
// non-matching Origin gets no CORS headers at all, while the request is
// still served (server-side enforcement is not this middleware's job).
func TestCORSDisallowedOriginGetsNoHeadersButStillServed(t *testing.T) {
	handler := CORS(staticOrigins("https://example.com"))(nextOK())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
	req.Header.Set("Origin", "https://evil.example.net")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (disallowed origin still served same-origin-style)", rec.Code)
	}
	for _, h := range []string{"Access-Control-Allow-Origin", "Vary", "Access-Control-Allow-Methods"} {
		if got := rec.Header().Get(h); got != "" {
			t.Fatalf("%s = %q, want unset for a disallowed origin", h, got)
		}
	}
}

// TestCORSNoOriginHeaderGetsNoCORSHeaders confirms a same-origin (no
// Origin header) request under the public scope gets no CORS headers.
func TestCORSNoOriginHeaderGetsNoCORSHeaders(t *testing.T) {
	handler := CORS(staticOrigins("https://example.com"))(nextOK())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want unset with no Origin header", got)
	}
}

// TestCORSPreflightReturns204 confirms an OPTIONS request to an in-scope
// path is short-circuited to a bare 204, with the allowed-origin headers
// applied when the origin matches.
func TestCORSPreflightReturns204(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	handler := CORS(staticOrigins("https://example.com"))(next)

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/posts", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", rec.Code)
	}
	if called {
		t.Fatal("preflight reached the wrapped handler, want it short-circuited")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("preflight Access-Control-Allow-Origin = %q, want %q", got, "https://example.com")
	}
}

// TestCORSOutOfScopePathUntouched confirms a path outside /api/v1/* and
// /api/files/* (e.g. the admin API) is never touched by this middleware,
// even with a matching Origin header and even for OPTIONS.
func TestCORSOutOfScopePathUntouched(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := CORS(staticOrigins("https://example.com"))(next)

	req := httptest.NewRequest(http.MethodOptions, "/api/admin/v1/posts", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("out-of-scope OPTIONS request did not reach the wrapped handler")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want unset for an out-of-scope path", got)
	}
}

// TestCORSFilesPathInScope confirms /api/files/* is in the CORS scope too
// (cover images loaded cross-origin by a product frontend).
func TestCORSFilesPathInScope(t *testing.T) {
	handler := CORS(staticOrigins("https://example.com"))(nextOK())

	req := httptest.NewRequest(http.MethodGet, "/api/files/abc/cover.png", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "https://example.com")
	}
}
