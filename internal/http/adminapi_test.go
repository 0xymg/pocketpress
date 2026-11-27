package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/media"
	"github.com/pocketpress/pocketpress/internal/store"
)

// testAppSecret is the fixed preview-token HMAC secret every test env is
// built with, so preview tests can mint tokens (valid, expired or tampered)
// with a matching preview.Tokenizer.
var testAppSecret = []byte("pocketpress-test-preview-secret")

// testEnv is a running admin API backed by a fresh temp-dir SQLite store,
// exercised over a real loopback HTTP server so the full middleware +
// handler + service + store chain is under test.
type testEnv struct {
	srv    *httptest.Server
	client *http.Client // carries a cookie jar (an authenticated "browser")
	db     *sql.DB
}

// newTestEnv builds a running admin API. secureCookies mirrors production
// (Secure cookie attribute) when true.
func newTestEnv(t *testing.T, secureCookies bool) *testEnv {
	t.Helper()

	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	if _, err := store.Up(context.Background(), st.DB()); err != nil {
		t.Fatalf("store.Up() error = %v", err)
	}

	mediaDriver, err := media.NewLocalDriver(dataDir)
	if err != nil {
		t.Fatalf("media.NewLocalDriver() error = %v", err)
	}

	handler := NewRouter(Config{
		Logger:        discardLogger(),
		Version:       "test",
		DB:            st.DB(),
		SecureCookies: secureCookies,
		MediaDriver:   mediaDriver,
		DataDir:       dataDir,
		MaxUploadMB:   10,
		AppSecret:     testAppSecret,
	})
	srv := httptest.NewServer(handler)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}

	env := &testEnv{srv: srv, client: &http.Client{Jar: jar}, db: st.DB()}
	t.Cleanup(func() {
		srv.Close()
		_ = st.Close()
	})
	return env
}

// do issues a request with the environment's cookie-jar client.
func (e *testEnv) do(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()
	return e.doWith(t, e.client, method, path, body)
}

// doWith issues a request with an explicit client (e.g. a second,
// unauthenticated "browser").
func (e *testEnv) doWith(t *testing.T, client *http.Client, method, path string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// readJSON reads the response body, returning the raw bytes and the decoded
// top-level object.
func readJSON(t *testing.T, resp *http.Response) ([]byte, map[string]any) {
	t.Helper()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	_ = resp.Body.Close()
	var m map[string]any
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("unmarshal body %q: %v", raw, err)
		}
	}
	return raw, m
}

// errorCode extracts error.code from a decoded envelope.
func errorCode(t *testing.T, m map[string]any) string {
	t.Helper()
	e, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("body has no error object: %v", m)
	}
	code, _ := e["code"].(string)
	return code
}

// validSetupBody is a complete, valid setup payload.
func validSetupBody() map[string]any {
	return map[string]any{
		"site_name":   "My Blog",
		"site_url":    "https://example.com",
		"owner_name":  "Owner",
		"owner_email": "owner@example.com",
		"password":    "a-strong-password",
		"timezone":    "UTC",
	}
}

// setup runs a valid setup and asserts a 201.
func (e *testEnv) setup(t *testing.T) {
	t.Helper()
	resp := e.do(t, http.MethodPost, "/api/admin/v1/setup", validSetupBody())
	if resp.StatusCode != http.StatusCreated {
		raw, _ := readJSON(t, resp)
		t.Fatalf("setup status = %d, want 201; body = %s", resp.StatusCode, raw)
	}
	_, _ = readJSON(t, resp)
}

// TestSetupFlow covers status → setup → status → me, and the already-set-up
// guard.
func TestSetupFlow(t *testing.T) {
	env := newTestEnv(t, false)

	// Fresh install reports not initialized.
	resp := env.do(t, http.MethodGet, "/api/admin/v1/setup/status", nil)
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK || m["initialized"] != false {
		t.Fatalf("setup/status (fresh) = %d %v, want 200 initialized:false", resp.StatusCode, m)
	}

	// Setup succeeds, sets a session cookie, returns the owner (no hash).
	resp = env.do(t, http.MethodPost, "/api/admin/v1/setup", validSetupBody())
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d, want 201; body = %s", resp.StatusCode, raw)
	}
	user, ok := m["user"].(map[string]any)
	if !ok {
		t.Fatalf("setup body has no user object: %s", raw)
	}
	if user["role"] != "owner" || user["email"] != "owner@example.com" {
		t.Fatalf("setup user = %v, want owner/owner@example.com", user)
	}
	if !hasSessionCookie(resp) {
		t.Fatal("setup did not set a pp_session cookie")
	}

	// Now initialized.
	resp = env.do(t, http.MethodGet, "/api/admin/v1/setup/status", nil)
	_, m = readJSON(t, resp)
	if m["initialized"] != true {
		t.Fatalf("setup/status (after) initialized = %v, want true", m["initialized"])
	}

	// The session cookie authenticates /auth/me.
	resp = env.do(t, http.MethodGet, "/api/admin/v1/auth/me", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me status = %d, want 200", resp.StatusCode)
	}
	if u, _ := m["user"].(map[string]any); u["email"] != "owner@example.com" {
		t.Fatalf("me user = %v, want owner@example.com", m["user"])
	}

	// Setup cannot run twice.
	resp = env.do(t, http.MethodPost, "/api/admin/v1/setup", validSetupBody())
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusForbidden || errorCode(t, m) != "already_initialized" {
		t.Fatalf("second setup = %d %s, want 403 already_initialized", resp.StatusCode, errorCode(t, m))
	}
}

// TestSetupValidation covers the field-level 400 validation envelope.
func TestSetupValidation(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(map[string]any)
		wantField string
	}{
		{"bad email", func(b map[string]any) { b["owner_email"] = "not-an-email" }, "owner_email"},
		{"short password", func(b map[string]any) { b["password"] = "short" }, "password"},
		{"bad timezone", func(b map[string]any) { b["timezone"] = "Mars/Phobos" }, "timezone"},
		{"bad site url", func(b map[string]any) { b["site_url"] = "notaurl" }, "site_url"},
		{"missing site name", func(b map[string]any) { delete(b, "site_name") }, "site_name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t, false)
			body := validSetupBody()
			tc.mutate(body)

			resp := env.do(t, http.MethodPost, "/api/admin/v1/setup", body)
			raw, m := readJSON(t, resp)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, raw)
			}
			if errorCode(t, m) != "validation_failed" {
				t.Fatalf("code = %q, want validation_failed", errorCode(t, m))
			}
			details, ok := m["error"].(map[string]any)["details"].(map[string]any)
			if !ok {
				t.Fatalf("details is not an object: %s", raw)
			}
			if _, ok := details[tc.wantField]; !ok {
				t.Fatalf("details missing field %q: %v", tc.wantField, details)
			}
		})
	}
}

// TestLoginLogoutFlow covers logout invalidating the session and login
// re-establishing it.
func TestLoginLogoutFlow(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	// Logout: 204, then me is 401.
	resp := env.do(t, http.MethodPost, "/api/admin/v1/auth/logout", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = env.do(t, http.MethodGet, "/api/admin/v1/auth/me", nil)
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusUnauthorized || errorCode(t, m) != "unauthorized" {
		t.Fatalf("me after logout = %d %s, want 401 unauthorized", resp.StatusCode, errorCode(t, m))
	}

	// Logout again still succeeds (no valid session).
	resp = env.do(t, http.MethodPost, "/api/admin/v1/auth/logout", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("second logout status = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Login: 200 + cookie, then me works again.
	resp = env.do(t, http.MethodPost, "/api/admin/v1/auth/login", map[string]any{
		"email": "owner@example.com", "password": "a-strong-password",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	if !hasSessionCookie(resp) {
		t.Fatal("login did not set a pp_session cookie")
	}
	_, _ = readJSON(t, resp)

	resp = env.do(t, http.MethodGet, "/api/admin/v1/auth/me", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me after login status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestLoginFailuresIdentical confirms unknown email, wrong password, and an
// inactive user yield byte-identical 401 bodies.
func TestLoginFailuresIdentical(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	// Create an editor and deactivate it, to exercise the inactive path.
	resp := env.do(t, http.MethodPost, "/api/admin/v1/users", map[string]any{
		"name": "Ed", "email": "editor@example.com", "password": "editor-strong-pass", "role": "editor",
	})
	_, m := readJSON(t, resp)
	editorID, _ := m["user"].(map[string]any)["id"].(string)
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/users/"+editorID, map[string]any{"active": false})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("deactivate editor status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	bodies := map[string][]byte{}
	for name, creds := range map[string]map[string]any{
		"unknown":     {"email": "nobody@example.com", "password": "a-strong-password"},
		"wrongpass":   {"email": "owner@example.com", "password": "wrong-password"},
		"deactivated": {"email": "editor@example.com", "password": "editor-strong-pass"},
	} {
		resp := env.do(t, http.MethodPost, "/api/admin/v1/auth/login", creds)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s login status = %d, want 401", name, resp.StatusCode)
		}
		raw, mm := readJSON(t, resp)
		if errorCode(t, mm) != "invalid_credentials" {
			t.Fatalf("%s login code = %q, want invalid_credentials", name, errorCode(t, mm))
		}
		bodies[name] = raw
	}
	if !bytes.Equal(bodies["unknown"], bodies["wrongpass"]) || !bytes.Equal(bodies["unknown"], bodies["deactivated"]) {
		t.Fatalf("login failure bodies differ:\n unknown=%s\n wrongpass=%s\n deactivated=%s",
			bodies["unknown"], bodies["wrongpass"], bodies["deactivated"])
	}
}

// TestUsersOwnerManagement covers the owner-only CRUD happy paths.
func TestUsersOwnerManagement(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	// One user (the owner) to start.
	resp := env.do(t, http.MethodGet, "/api/admin/v1/users", nil)
	_, m := readJSON(t, resp)
	if users, _ := m["users"].([]any); len(users) != 1 {
		t.Fatalf("initial user count = %d, want 1", len(m["users"].([]any)))
	}

	// Create an editor.
	resp = env.do(t, http.MethodPost, "/api/admin/v1/users", map[string]any{
		"name": "Editor", "email": "editor@example.com", "password": "editor-strong-pass", "role": "editor",
	})
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create user status = %d, want 201; body = %s", resp.StatusCode, raw)
	}
	editorID, _ := m["user"].(map[string]any)["id"].(string)
	if editorID == "" {
		t.Fatalf("created user has no id: %s", raw)
	}

	// Now two users.
	resp = env.do(t, http.MethodGet, "/api/admin/v1/users", nil)
	_, m = readJSON(t, resp)
	if users, _ := m["users"].([]any); len(users) != 2 {
		t.Fatalf("user count after create = %d, want 2", len(users))
	}

	// Duplicate email is a validation error.
	resp = env.do(t, http.MethodPost, "/api/admin/v1/users", map[string]any{
		"name": "Dup", "email": "editor@example.com", "password": "another-strong-pass", "role": "editor",
	})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "validation_failed" {
		t.Fatalf("duplicate email = %d %s, want 400 validation_failed", resp.StatusCode, errorCode(t, m))
	}

	// PATCH the editor: rename and deactivate.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/users/"+editorID, map[string]any{
		"name": "Renamed", "active": false,
	})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d, want 200", resp.StatusCode)
	}
	u, _ := m["user"].(map[string]any)
	if u["name"] != "Renamed" || u["active"] != false {
		t.Fatalf("patched user = %v, want name Renamed active false", u)
	}

	// PATCH an unknown id → 404.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/users/does-not-exist", map[string]any{"name": "X"})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, m) != "not_found" {
		t.Fatalf("patch unknown = %d %s, want 404 not_found", resp.StatusCode, errorCode(t, m))
	}
}

// TestEditorForbiddenOnUsers confirms a logged-in editor gets 403 on the
// user-management endpoints but can still call /auth/me.
func TestEditorForbiddenOnUsers(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	resp := env.do(t, http.MethodPost, "/api/admin/v1/users", map[string]any{
		"name": "Editor", "email": "editor@example.com", "password": "editor-strong-pass", "role": "editor",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create editor status = %d, want 201", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// A second "browser" logs in as the editor.
	jar, _ := cookiejar.New(nil)
	editorClient := &http.Client{Jar: jar}
	resp = env.doWith(t, editorClient, http.MethodPost, "/api/admin/v1/auth/login", map[string]any{
		"email": "editor@example.com", "password": "editor-strong-pass",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("editor login status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Editor can see itself.
	resp = env.doWith(t, editorClient, http.MethodGet, "/api/admin/v1/auth/me", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("editor me status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// But is forbidden from user management.
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/admin/v1/users", nil},
		{http.MethodPost, "/api/admin/v1/users", map[string]any{"name": "X", "email": "x@example.com", "password": "x-strong-pass1", "role": "editor"}},
		{http.MethodPatch, "/api/admin/v1/users/some-id", map[string]any{"name": "Y"}},
	} {
		resp := env.doWith(t, editorClient, tc.method, tc.path, tc.body)
		_, m := readJSON(t, resp)
		if resp.StatusCode != http.StatusForbidden || errorCode(t, m) != "forbidden" {
			t.Fatalf("%s %s as editor = %d %s, want 403 forbidden", tc.method, tc.path, resp.StatusCode, errorCode(t, m))
		}
	}
}

// TestUsersUnauthenticated confirms the protected endpoints 401 without a
// session.
func TestUsersUnauthenticated(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	// A bare client with no cookies.
	anon := &http.Client{}
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/admin/v1/users"},
		{http.MethodPost, "/api/admin/v1/users"},
		{http.MethodGet, "/api/admin/v1/auth/me"},
	} {
		resp := env.doWith(t, anon, tc.method, tc.path, nil)
		_, m := readJSON(t, resp)
		if resp.StatusCode != http.StatusUnauthorized || errorCode(t, m) != "unauthorized" {
			t.Fatalf("%s %s anon = %d %s, want 401 unauthorized", tc.method, tc.path, resp.StatusCode, errorCode(t, m))
		}
	}
}

// TestLastOwnerGuard confirms the last active owner cannot be deactivated or
// demoted.
func TestLastOwnerGuard(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	owner, err := store.GetUserByEmail(context.Background(), env.db, "owner@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail() error = %v", err)
	}

	for _, body := range []map[string]any{
		{"active": false},
		{"role": "editor"},
	} {
		resp := env.do(t, http.MethodPatch, "/api/admin/v1/users/"+owner.ID, body)
		_, m := readJSON(t, resp)
		if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "last_owner" {
			t.Fatalf("patch %v on last owner = %d %s, want 400 last_owner", body, resp.StatusCode, errorCode(t, m))
		}
	}
}

// TestCookieFlags confirms the session cookie flags in both non-production
// (Secure off) and production (Secure on) configurations.
func TestCookieFlags(t *testing.T) {
	t.Run("non-production", func(t *testing.T) {
		env := newTestEnv(t, false)
		resp := env.do(t, http.MethodPost, "/api/admin/v1/setup", validSetupBody())
		_, _ = readJSON(t, resp)
		c := sessionCookie(t, resp)
		if !c.HttpOnly {
			t.Error("cookie HttpOnly = false, want true")
		}
		if c.SameSite != http.SameSiteLaxMode {
			t.Errorf("cookie SameSite = %v, want Lax", c.SameSite)
		}
		if c.Secure {
			t.Error("cookie Secure = true in non-production, want false")
		}
	})

	t.Run("production", func(t *testing.T) {
		env := newTestEnv(t, true)
		resp := env.do(t, http.MethodPost, "/api/admin/v1/setup", validSetupBody())
		_, _ = readJSON(t, resp)
		c := sessionCookie(t, resp)
		if !c.Secure {
			t.Error("cookie Secure = false in production, want true")
		}
		if !c.HttpOnly {
			t.Error("cookie HttpOnly = false, want true")
		}
	})
}

// TestPasswordHashNeverLeaks scans raw response bodies of every
// user-returning endpoint for password material.
func TestPasswordHashNeverLeaks(t *testing.T) {
	env := newTestEnv(t, false)

	check := func(label string, raw []byte) {
		s := string(raw)
		if strings.Contains(s, "password_hash") || strings.Contains(s, "argon2") || strings.Contains(s, "a-strong-password") {
			t.Fatalf("%s response leaked password material: %s", label, s)
		}
	}

	resp := env.do(t, http.MethodPost, "/api/admin/v1/setup", validSetupBody())
	raw, _ := readJSON(t, resp)
	check("setup", raw)

	resp = env.do(t, http.MethodGet, "/api/admin/v1/auth/me", nil)
	raw, _ = readJSON(t, resp)
	check("me", raw)

	resp = env.do(t, http.MethodPost, "/api/admin/v1/users", map[string]any{
		"name": "Editor", "email": "editor@example.com", "password": "editor-strong-pass", "role": "editor",
	})
	raw, _ = readJSON(t, resp)
	check("create user", raw)

	resp = env.do(t, http.MethodGet, "/api/admin/v1/users", nil)
	raw, _ = readJSON(t, resp)
	check("list users", raw)

	resp = env.do(t, http.MethodPost, "/api/admin/v1/auth/login", map[string]any{
		"email": "owner@example.com", "password": "a-strong-password",
	})
	raw, _ = readJSON(t, resp)
	check("login", raw)
}

// TestSessionExpiryRejected inserts an already-expired session directly and
// confirms it does not authenticate.
func TestSessionExpiryRejected(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	owner, err := store.GetUserByEmail(context.Background(), env.db, "owner@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail() error = %v", err)
	}

	token := "expired-session-token-value"
	sum := sha256.Sum256([]byte(token))
	if _, err := store.CreateSession(context.Background(), env.db, store.NewSession{
		UserID:    owner.ID,
		TokenHash: hex.EncodeToString(sum[:]),
		ExpiresAt: time.Now().UTC().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	anon := &http.Client{}
	req, _ := http.NewRequest(http.MethodGet, env.srv.URL+"/api/admin/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: "pp_session", Value: token})
	resp, err := anon.Do(req)
	if err != nil {
		t.Fatalf("me with expired cookie: %v", err)
	}
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusUnauthorized || errorCode(t, m) != "unauthorized" {
		t.Fatalf("me with expired session = %d %s, want 401 unauthorized", resp.StatusCode, errorCode(t, m))
	}
}

// hasSessionCookie reports whether resp sets a non-empty pp_session cookie.
func hasSessionCookie(resp *http.Response) bool {
	for _, c := range resp.Cookies() {
		if c.Name == "pp_session" && c.Value != "" {
			return true
		}
	}
	return false
}

// sessionCookie returns the pp_session cookie set by resp, failing if none.
func sessionCookie(t *testing.T, resp *http.Response) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == "pp_session" {
			return c
		}
	}
	t.Fatal("no pp_session cookie in response")
	return nil
}
