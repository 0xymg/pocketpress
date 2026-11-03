package http

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestSecurityHeadersAcrossResponseClasses spot-checks that the common security
// headers ride on every response class (SPA HTML, admin API JSON, file
// serving) and that the CSP is scoped to the SPA only.
func TestSecurityHeadersAcrossResponseClasses(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	assertCommon := func(t *testing.T, resp *http.Response, label string) {
		t.Helper()
		for k, want := range map[string]string{
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":        "DENY",
			"Referrer-Policy":        "strict-origin-when-cross-origin",
		} {
			if got := resp.Header.Get(k); got != want {
				t.Fatalf("%s: %s = %q, want %q", label, k, got, want)
			}
		}
	}

	// SPA HTML.
	spa := env.do(t, http.MethodGet, "/", nil)
	assertCommon(t, spa, "SPA")
	if csp := spa.Header.Get("Content-Security-Policy"); csp == "" {
		t.Fatal("SPA response is missing a Content-Security-Policy header")
	}
	_ = spa.Body.Close()

	// Admin API JSON.
	api := env.do(t, http.MethodGet, "/api/admin/v1/auth/me", nil)
	assertCommon(t, api, "API")
	if csp := api.Header.Get("Content-Security-Policy"); csp != "" {
		t.Fatalf("API response carries a CSP header %q, want none", csp)
	}
	_ = api.Body.Close()

	// File serving: upload an image, then fetch it.
	m := env.createMedia(t, "cover.png", pngBytes(t))
	fileURL, _ := m["url"].(string)
	if fileURL == "" {
		t.Fatal("uploaded media has no url")
	}
	file := env.do(t, http.MethodGet, fileURL, nil)
	assertCommon(t, file, "file")
	if cc := file.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("file Cache-Control = %q, want the existing immutable caching preserved", cc)
	}
	_ = file.Body.Close()
}

// TestSPACacheHeaders locks in the SPA cache policy: the app shell (index.html
// and any client-side-route fallback) is revalidated on every load, while
// content-hashed build assets under /_app/immutable/ are cached immutably. This
// prevents a browser from running a stale index.html whose inline bootstrap and
// startup-computed CSP hash no longer match a freshly built binary.
func TestSPACacheHeaders(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	// index.html shell.
	idx := env.do(t, http.MethodGet, "/", nil)
	body, err := io.ReadAll(idx.Body)
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	_ = idx.Body.Close()
	if cc := idx.Header.Get("Cache-Control"); !strings.Contains(cc, "no-cache") {
		t.Fatalf("GET / Cache-Control = %q, want no-cache", cc)
	}

	// A client-side route that falls through to the shell must also revalidate.
	fb := env.do(t, http.MethodGet, "/posts/new", nil)
	if cc := fb.Header.Get("Cache-Control"); !strings.Contains(cc, "no-cache") {
		t.Fatalf("GET /posts/new (SPA fallback) Cache-Control = %q, want no-cache", cc)
	}
	_ = fb.Body.Close()

	// A real content-hashed asset must be cached immutably.
	asset := firstImmutableAssetPath(body)
	if asset == "" {
		t.Fatal("no /_app/immutable/ asset referenced by index.html; cannot test immutable caching")
	}
	res := env.do(t, http.MethodGet, asset, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", asset, res.StatusCode)
	}
	if cc := res.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("GET %s Cache-Control = %q, want immutable", asset, cc)
	}
	_ = res.Body.Close()
}

// firstImmutableAssetPath extracts the first /_app/immutable/... URL referenced
// in the served index.html (a hashed JS/CSS entry), or "" if none.
func firstImmutableAssetPath(html []byte) string {
	s := string(html)
	i := strings.Index(s, immutableAssetPrefix)
	if i < 0 {
		return ""
	}
	end := i
	for end < len(s) && s[end] != '"' && s[end] != '\'' && s[end] != ' ' && s[end] != '>' {
		end++
	}
	return s[i:end]
}

// TestSPACSPMatchesBuiltAssets parses the actually-served index.html for inline
// scripts and asserts every one of their hashes is present in the CSP, so the
// real built admin SPA is not blocked by its own policy.
func TestSPACSPMatchesBuiltAssets(t *testing.T) {
	handler := NewRouter(Config{Logger: discardLogger(), Version: "test"})

	env := newTestEnv(t, false)
	env.setup(t)
	resp := env.do(t, http.MethodGet, "/", nil)
	csp := resp.Header.Get("Content-Security-Policy")
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	_ = resp.Body.Close()

	if csp == "" {
		t.Fatal("GET / returned no CSP header")
	}

	// The header must equal what buildSPACSP produces from the embedded assets.
	if want := buildSPACSP(); csp != want {
		t.Fatalf("served CSP = %q, want %q", csp, want)
	}

	// script-src must always at least allow same-origin scripts.
	if !strings.Contains(csp, "script-src 'self'") {
		t.Fatalf("CSP %q missing \"script-src 'self'\"", csp)
	}

	// Every inline <script> in the served HTML must have its sha256 in the CSP,
	// or the browser would block the SvelteKit bootstrap.
	hashes := inlineScriptHashes(body)
	if len(hashes) == 0 {
		t.Fatal("no inline scripts found in served index.html; the CSP hashing path is untested")
	}
	for _, h := range hashes {
		if !strings.Contains(csp, h) {
			t.Fatalf("inline script hash %s is not present in the CSP %q — the SPA would be blocked", h, csp)
		}
	}

	_ = handler // constructed above only to prove NewRouter builds with a CSP-bearing SPA handler
}

// TestLoginRateLimitReturns429ThroughRouter drives the real router: after the
// (successful, bucket-clearing) setup, five failed logins are allowed and the
// sixth is throttled with a 429 + Retry-After.
func TestLoginRateLimitReturns429ThroughRouter(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	badCreds := map[string]any{"email": "owner@example.com", "password": "wrong-password"}
	for i := 1; i <= 5; i++ {
		resp := env.do(t, http.MethodPost, "/api/admin/v1/auth/login", badCreds)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("failed login %d status = %d, want 401", i, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	resp := env.do(t, http.MethodPost, "/api/admin/v1/auth/login", badCreds)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("6th login status = %d, want 429", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("429 response missing Retry-After header")
	}
	_, m := readJSON(t, resp)
	if errorCode(t, m) != "rate_limited" {
		t.Fatalf("429 error code = %q, want rate_limited", errorCode(t, m))
	}
}

// TestOversizedBodyRejectedWithEnvelope confirms the global body cap: an
// oversized request to a small endpoint is rejected with a JSON envelope rather
// than being buffered wholesale.
func TestOversizedBodyRejectedWithEnvelope(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	// A login body well over the 1 MiB default cap.
	huge := strings.Repeat("a", (1<<20)+1024)
	resp := env.do(t, http.MethodPost, "/api/admin/v1/auth/login", map[string]any{
		"email":    "owner@example.com",
		"password": huge,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized login status = %d, want 400", resp.StatusCode)
	}
	_, m := readJSON(t, resp)
	if code := errorCode(t, m); code != "bad_request" {
		t.Fatalf("oversized login error code = %q, want bad_request", code)
	}
}
