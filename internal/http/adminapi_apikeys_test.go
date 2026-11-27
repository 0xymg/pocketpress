package http

import (
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"github.com/pocketpress/pocketpress/internal/apikey"
)

func (e *testEnv) createAPIKeyViaAPI(t *testing.T, body map[string]any) (id, raw string) {
	t.Helper()
	resp := e.do(t, http.MethodPost, "/api/admin/v1/api-keys", body)
	rawBody, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create api key status = %d, want 201; body = %s", resp.StatusCode, rawBody)
	}
	key, ok := m["api_key"].(map[string]any)
	if !ok {
		t.Fatalf("create body missing api_key: %s", rawBody)
	}
	id, _ = key["id"].(string)
	raw, _ = m["key"].(string)
	if id == "" || raw == "" {
		t.Fatalf("create missing id/key: %s", rawBody)
	}
	return id, raw
}

func TestAPIKeyCRUDAndKeyOnce(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	id, raw := env.createAPIKeyViaAPI(t, map[string]any{
		"name":   "CI reader",
		"scopes": []string{"content:read", "preview:read"},
	})
	if !strings.HasPrefix(raw, apikey.KeyPrefix) {
		t.Fatalf("key = %q, want ppkey_ prefix", raw)
	}

	resp := env.do(t, http.MethodGet, "/api/admin/v1/api-keys", nil)
	rawBody, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", resp.StatusCode)
	}
	if strings.Contains(string(rawBody), raw) || strings.Contains(string(rawBody), `"key"`) {
		t.Fatalf("list leaked raw key material: %s", rawBody)
	}
	list, _ := m["api_keys"].([]any)
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
	first := list[0].(map[string]any)
	if first["prefix"] == nil || first["revoked_at"] != nil {
		t.Fatalf("list item = %v", first)
	}

	resp = env.do(t, http.MethodPost, "/api/admin/v1/api-keys/"+id+"/revoke", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke status = %d, want 200", resp.StatusCode)
	}
	ak := m["api_key"].(map[string]any)
	if ak["revoked_at"] == nil {
		t.Fatalf("revoke left revoked_at nil: %v", ak)
	}

	resp = env.do(t, http.MethodDelete, "/api/admin/v1/api-keys/"+id, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = env.do(t, http.MethodGet, "/api/admin/v1/api-keys", nil)
	_, m = readJSON(t, resp)
	if list, _ := m["api_keys"].([]any); len(list) != 0 {
		t.Fatalf("list after delete len = %d, want 0", len(list))
	}
}

func TestAPIKeyCreateValidation(t *testing.T) {
	cases := []struct {
		name      string
		body      map[string]any
		wantField string
	}{
		{"missing name", map[string]any{"scopes": []string{"content:read"}}, "name"},
		{"missing scopes", map[string]any{"name": "X"}, "scopes"},
		{"bad scope", map[string]any{"name": "X", "scopes": []string{"admin:write"}}, "scopes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t, false)
			env.setup(t)
			resp := env.do(t, http.MethodPost, "/api/admin/v1/api-keys", tc.body)
			raw, m := readJSON(t, resp)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, raw)
			}
			if errorCode(t, m) != "validation_failed" {
				t.Fatalf("code = %q", errorCode(t, m))
			}
			details, ok := m["error"].(map[string]any)["details"].(map[string]any)
			if !ok {
				t.Fatalf("details missing: %s", raw)
			}
			if _, ok := details[tc.wantField]; !ok {
				t.Fatalf("details missing %q: %v", tc.wantField, details)
			}
		})
	}
}

func TestAPIKeyOwnerOnly(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	resp := env.do(t, http.MethodPost, "/api/admin/v1/users", map[string]any{
		"name": "Editor", "email": "editor@example.com", "password": "editor-strong-pass", "role": "editor",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create editor status = %d, want 201", resp.StatusCode)
	}
	_, _ = readJSON(t, resp)

	jar, _ := cookiejar.New(nil)
	editor := &http.Client{Jar: jar}
	resp = env.doWith(t, editor, http.MethodPost, "/api/admin/v1/auth/login", map[string]any{
		"email": "editor@example.com", "password": "editor-strong-pass",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("editor login status = %d, want 200", resp.StatusCode)
	}
	_, _ = readJSON(t, resp)

	resp = env.doWith(t, editor, http.MethodGet, "/api/admin/v1/api-keys", nil)
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusForbidden || errorCode(t, m) != "forbidden" {
		t.Fatalf("GET api-keys as editor = %d %s, want 403 forbidden", resp.StatusCode, errorCode(t, m))
	}
	resp = env.doWith(t, editor, http.MethodPost, "/api/admin/v1/api-keys", map[string]any{
		"name": "Nope", "scopes": []string{"content:read"},
	})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusForbidden || errorCode(t, m) != "forbidden" {
		t.Fatalf("POST api-keys as editor = %d %s, want 403 forbidden", resp.StatusCode, errorCode(t, m))
	}
}

func TestPrivateContentAPIWithAPIKey(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	post := env.createPost(t, map[string]any{"title": "Hello", "slug": "hello"})
	postID, _ := post["id"].(string)
	resp := env.do(t, http.MethodPost, "/api/admin/v1/posts/"+postID+"/publish", nil)
	if resp.StatusCode != http.StatusOK {
		raw, _ := readJSON(t, resp)
		t.Fatalf("publish = %d %s", resp.StatusCode, raw)
	}
	_, _ = readJSON(t, resp)

	_, contentKey := env.createAPIKeyViaAPI(t, map[string]any{
		"name": "content", "scopes": []string{"content:read"},
	})
	_, previewKey := env.createAPIKeyViaAPI(t, map[string]any{
		"name": "preview", "scopes": []string{"preview:read"},
	})

	env.setPublicAPIEnabled(t, false)

	resp = env.do(t, http.MethodGet, "/api/v1/posts", nil)
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, m) != "api_disabled" {
		t.Fatalf("unauth disabled = %d %s", resp.StatusCode, errorCode(t, m))
	}

	req, _ := http.NewRequest(http.MethodGet, env.srv.URL+"/api/v1/posts", nil)
	req.Header.Set("Authorization", "Bearer ppkey_not-real")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusUnauthorized || errorCode(t, m) != "unauthorized" {
		t.Fatalf("bad key = %d %s", resp.StatusCode, errorCode(t, m))
	}

	req, _ = http.NewRequest(http.MethodGet, env.srv.URL+"/api/v1/posts", nil)
	req.Header.Set("Authorization", "Bearer "+previewKey)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusForbidden || errorCode(t, m) != "forbidden" {
		t.Fatalf("wrong scope = %d %s", resp.StatusCode, errorCode(t, m))
	}

	req, _ = http.NewRequest(http.MethodGet, env.srv.URL+"/api/v1/posts", nil)
	req.Header.Set("Authorization", "Bearer "+contentKey)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("content key = %d %s", resp.StatusCode, raw)
	}
	if got := resp.Header.Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q, want private, no-store", got)
	}
	if data, _ := m["data"].([]any); len(data) != 1 {
		t.Fatalf("data len = %d, want 1", len(data))
	}

	req, _ = http.NewRequest(http.MethodGet, env.srv.URL+"/api/v1/preview/posts/hello", nil)
	req.Header.Set("Authorization", "Bearer "+previewKey)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview with key = %d %s", resp.StatusCode, raw)
	}

	req, _ = http.NewRequest(http.MethodGet, env.srv.URL+"/api/v1/preview/posts/hello", nil)
	req.Header.Set("Authorization", "Bearer "+contentKey)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("content key preview = %d %s", resp.StatusCode, errorCode(t, m))
	}

	var blob string
	if err := env.db.QueryRow(`SELECT group_concat(prefix||token_hash) FROM api_keys`).Scan(&blob); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(blob, contentKey) || strings.Contains(blob, previewKey) {
		t.Fatalf("raw keys stored in DB: %q", blob)
	}
}
