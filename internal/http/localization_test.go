package http

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// TestLocalizationLocaleFilterAndTranslations covers the v0.3 localization
// slice: per-post locale, ?locale= filter, translation linking, and
// available_locales on settings.
func TestLocalizationLocaleFilterAndTranslations(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	now := time.Now().UTC()

	// Enable Turkish on the site.
	resp := env.do(t, http.MethodPatch, "/api/admin/v1/settings", map[string]any{
		"available_locales": []string{"en", "tr"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /settings status = %d, want 200", resp.StatusCode)
	}

	en := env.createPost(t, map[string]any{
		"title": "Hello", "slug": "hello-en", "locale": "en",
	})
	env.publishPost(t, en["id"].(string), now.Add(-time.Minute))

	tr := env.createPost(t, map[string]any{
		"title": "Merhaba", "slug": "hello-tr", "locale": "tr",
		"translate_of": en["id"].(string),
	})
	env.publishPost(t, tr["id"].(string), now.Add(-30*time.Second))

	// Public list filtered by locale.
	resp = env.do(t, http.MethodGet, "/api/v1/posts?locale=tr", nil)
	_, m := readJSON(t, resp)
	data, _ := m["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("GET /posts?locale=tr len = %d, want 1", len(data))
	}
	if data[0].(map[string]any)["slug"] != "hello-tr" {
		t.Fatalf("locale=tr slug = %v, want hello-tr", data[0])
	}

	resp = env.do(t, http.MethodGet, "/api/v1/posts?locale=en", nil)
	_, m = readJSON(t, resp)
	data, _ = m["data"].([]any)
	if len(data) != 1 || data[0].(map[string]any)["slug"] != "hello-en" {
		t.Fatalf("locale=en data = %v, want hello-en only", data)
	}

	// Detail includes locale + published translations.
	resp = env.do(t, http.MethodGet, "/api/v1/posts/hello-en", nil)
	_, m = readJSON(t, resp)
	post := dataObj(t, m)
	if post["locale"] != "en" {
		t.Fatalf("hello-en locale = %v, want en", post["locale"])
	}
	translations, _ := post["translations"].([]any)
	if len(translations) != 1 {
		t.Fatalf("hello-en translations = %v, want 1 sibling", translations)
	}
	sib := translations[0].(map[string]any)
	if sib["locale"] != "tr" || sib["slug"] != "hello-tr" {
		t.Fatalf("translation sibling = %v, want tr/hello-tr", sib)
	}

	// Reject unknown locale on create.
	resp = env.do(t, http.MethodPost, "/api/admin/v1/posts", map[string]any{
		"title": "Deutsch", "slug": "hello-de", "locale": "de",
	})
	if resp.StatusCode != http.StatusBadRequest {
		_, m := readJSON(t, resp)
		raw, _ := json.Marshal(m)
		t.Fatalf("POST locale=de status = %d body=%s, want 400", resp.StatusCode, raw)
	}
}

// TestNormalizeLocaleViaPublicFilter confirms a malformed locale filter
// returns an empty page rather than 500.
func TestLocalizationMalformedLocaleFilter(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	now := time.Now().UTC()
	p := env.createPost(t, map[string]any{"title": "X", "slug": "x"})
	env.publishPost(t, p["id"].(string), now.Add(-time.Minute))

	resp := env.do(t, http.MethodGet, "/api/v1/posts?locale=!!!", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	_, m := readJSON(t, resp)
	data, _ := m["data"].([]any)
	if len(data) != 0 {
		t.Fatalf("malformed locale filter returned %d posts, want 0", len(data))
	}
}
