package http

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/preview"
)

// ---------------------------------------------------------------------------
// Transition helpers (build on testEnv from adminapi_test.go)
// ---------------------------------------------------------------------------

// action POSTs to /posts/{id}/{action} and returns the response + decoded body.
func (e *testEnv) action(t *testing.T, id, action string, body any) (*http.Response, map[string]any) {
	t.Helper()
	resp := e.do(t, http.MethodPost, "/api/admin/v1/posts/"+id+"/"+action, body)
	_, m := readJSON(t, resp)
	return resp, m
}

// mustAction runs an action expecting 200 and returns the post object.
func (e *testEnv) mustAction(t *testing.T, id, action string, body any) map[string]any {
	t.Helper()
	resp, m := e.action(t, id, action, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s on %s = %d, want 200; body = %v", action, id, resp.StatusCode, m)
	}
	post, ok := m["post"].(map[string]any)
	if !ok {
		t.Fatalf("%s response has no post object: %v", action, m)
	}
	return post
}

func futureRFC3339(d time.Duration) string {
	return time.Now().UTC().Add(d).Format(time.RFC3339)
}

// makePost creates a draft and drives it into the requested status.
func (e *testEnv) makePost(t *testing.T, title, slug, status string) string {
	t.Helper()
	p := e.createPost(t, map[string]any{"title": title, "slug": slug})
	id := p["id"].(string)
	switch status {
	case "draft":
	case "published":
		e.mustAction(t, id, "publish", nil)
	case "scheduled":
		e.mustAction(t, id, "schedule", map[string]any{"scheduled_for": futureRFC3339(time.Hour)})
	case "archived":
		e.mustAction(t, id, "archive", nil)
	default:
		t.Fatalf("unknown seed status %q", status)
	}
	return id
}

// ---------------------------------------------------------------------------
// Publish
// ---------------------------------------------------------------------------

// TestPublishFromEveryStatus covers publish from draft/scheduled/archived and
// the idempotent no-op from published, plus scheduled_for clearing.
func TestPublishFromEveryStatus(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	for i, from := range []string{"draft", "scheduled", "archived", "published"} {
		id := env.makePost(t, "Pub "+from, "pub-"+string(rune('a'+i)), from)
		post := env.mustAction(t, id, "publish", nil)
		if post["status"] != "published" {
			t.Fatalf("publish from %s: status = %v, want published", from, post["status"])
		}
		if post["published_at"] == nil {
			t.Fatalf("publish from %s: published_at is null, want set", from)
		}
		if post["scheduled_for"] != nil {
			t.Fatalf("publish from %s: scheduled_for = %v, want cleared", from, post["scheduled_for"])
		}
	}
}

// TestPublishedAtSetOnceHTTP confirms publish→unpublish→publish keeps the
// first published_at across the whole cycle.
func TestPublishedAtSetOnceHTTP(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	id := env.makePost(t, "Once", "once", "draft")

	first := env.mustAction(t, id, "publish", nil)["published_at"]
	if first == nil {
		t.Fatal("first publish published_at is null")
	}

	back := env.mustAction(t, id, "unpublish", nil)
	if back["status"] != "draft" {
		t.Fatalf("unpublish status = %v, want draft", back["status"])
	}
	if back["published_at"] != first {
		t.Fatalf("unpublish published_at = %v, want preserved %v", back["published_at"], first)
	}

	again := env.mustAction(t, id, "publish", nil)
	if again["published_at"] != first {
		t.Fatalf("re-publish published_at = %v, want original %v", again["published_at"], first)
	}
}

// ---------------------------------------------------------------------------
// Unpublish
// ---------------------------------------------------------------------------

func TestUnpublishFromEveryStatus(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	// Allowed from published/scheduled/archived → draft.
	for i, from := range []string{"published", "scheduled", "archived"} {
		id := env.makePost(t, "Unpub "+from, "unpub-"+string(rune('a'+i)), from)
		post := env.mustAction(t, id, "unpublish", nil)
		if post["status"] != "draft" {
			t.Fatalf("unpublish from %s: status = %v, want draft", from, post["status"])
		}
		if post["scheduled_for"] != nil {
			t.Fatalf("unpublish from %s: scheduled_for = %v, want cleared", from, post["scheduled_for"])
		}
	}

	// Rejected from draft.
	draft := env.makePost(t, "Unpub draft", "unpub-draft", "draft")
	resp, m := env.action(t, draft, "unpublish", nil)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "validation_failed" {
		t.Fatalf("unpublish draft = %d %s, want 400 validation_failed", resp.StatusCode, errorCode(t, m))
	}
	if _, ok := validationDetails(t, m)["status"]; !ok {
		t.Fatalf("unpublish draft details missing status: %v", m)
	}
}

// ---------------------------------------------------------------------------
// Schedule
// ---------------------------------------------------------------------------

func TestScheduleTransitions(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	// From draft → scheduled with scheduled_for set.
	id := env.makePost(t, "Sched", "sched", "draft")
	post := env.mustAction(t, id, "schedule", map[string]any{"scheduled_for": futureRFC3339(time.Hour)})
	if post["status"] != "scheduled" || post["scheduled_for"] == nil {
		t.Fatalf("schedule from draft = status %v scheduled_for %v, want scheduled + set", post["status"], post["scheduled_for"])
	}
	firstWhen := post["scheduled_for"]

	// Re-schedule from scheduled changes the time.
	post = env.mustAction(t, id, "schedule", map[string]any{"scheduled_for": futureRFC3339(3 * time.Hour)})
	if post["scheduled_for"] == firstWhen {
		t.Fatalf("re-schedule scheduled_for unchanged = %v, want a new time", post["scheduled_for"])
	}

	// From archived → scheduled.
	arch := env.makePost(t, "Sched arch", "sched-arch", "archived")
	post = env.mustAction(t, arch, "schedule", map[string]any{"scheduled_for": futureRFC3339(time.Hour)})
	if post["status"] != "scheduled" {
		t.Fatalf("schedule from archived status = %v, want scheduled", post["status"])
	}

	// Past time → 400 details.scheduled_for.
	past := env.makePost(t, "Sched past", "sched-past", "draft")
	resp, m := env.action(t, past, "schedule", map[string]any{"scheduled_for": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)})
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "validation_failed" {
		t.Fatalf("schedule past = %d %s, want 400 validation_failed", resp.StatusCode, errorCode(t, m))
	}
	if _, ok := validationDetails(t, m)["scheduled_for"]; !ok {
		t.Fatalf("schedule past details missing scheduled_for: %v", m)
	}

	// Malformed timestamp → 400 details.scheduled_for.
	resp, m = env.action(t, past, "schedule", map[string]any{"scheduled_for": "not-a-date"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("schedule bad time = %d, want 400", resp.StatusCode)
	}
	if _, ok := validationDetails(t, m)["scheduled_for"]; !ok {
		t.Fatalf("schedule bad time details missing scheduled_for: %v", m)
	}

	// From published → 400 details.status.
	pub := env.makePost(t, "Sched pub", "sched-pub", "published")
	resp, m = env.action(t, pub, "schedule", map[string]any{"scheduled_for": futureRFC3339(time.Hour)})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("schedule published = %d, want 400", resp.StatusCode)
	}
	if _, ok := validationDetails(t, m)["status"]; !ok {
		t.Fatalf("schedule published details missing status: %v", m)
	}
}

// ---------------------------------------------------------------------------
// Archive
// ---------------------------------------------------------------------------

func TestArchiveFromEveryStatus(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	for i, from := range []string{"draft", "scheduled", "published", "archived"} {
		id := env.makePost(t, "Arch "+from, "arch-"+string(rune('a'+i)), from)
		wantPublishedAt := env.mustGet(t, id)["published_at"]
		post := env.mustAction(t, id, "archive", nil)
		if post["status"] != "archived" {
			t.Fatalf("archive from %s: status = %v, want archived", from, post["status"])
		}
		if from == "published" && post["published_at"] != wantPublishedAt {
			t.Fatalf("archive from published: published_at = %v, want preserved %v", post["published_at"], wantPublishedAt)
		}
	}
}

// mustGet fetches a post via the admin API, expecting 200.
func (e *testEnv) mustGet(t *testing.T, id string) map[string]any {
	t.Helper()
	resp := e.do(t, http.MethodGet, "/api/admin/v1/posts/"+id, nil)
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get post %s = %d, want 200", id, resp.StatusCode)
	}
	return m["post"].(map[string]any)
}

// ---------------------------------------------------------------------------
// Transition edge cases
// ---------------------------------------------------------------------------

func TestTransitionsNotFoundAndUnauthenticated(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	for _, action := range []string{"publish", "unpublish", "schedule", "archive", "preview-token"} {
		resp, m := env.action(t, "does-not-exist", action, map[string]any{"scheduled_for": futureRFC3339(time.Hour)})
		if resp.StatusCode != http.StatusNotFound || errorCode(t, m) != "not_found" {
			t.Fatalf("%s unknown id = %d %s, want 404 not_found", action, resp.StatusCode, errorCode(t, m))
		}
	}

	anon := &http.Client{}
	for _, action := range []string{"publish", "unpublish", "schedule", "archive", "preview-token"} {
		resp := env.doWith(t, anon, http.MethodPost, "/api/admin/v1/posts/x/"+action, nil)
		_, m := readJSON(t, resp)
		if resp.StatusCode != http.StatusUnauthorized || errorCode(t, m) != "unauthorized" {
			t.Fatalf("%s anon = %d %s, want 401 unauthorized", action, resp.StatusCode, errorCode(t, m))
		}
	}
}

// TestUpdateKeepsPublishedStatus confirms a normal PATCH on a published post
// keeps it published (Save on live content) and never transitions status.
func TestUpdateKeepsPublishedStatus(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	id := env.makePost(t, "Live", "live", "published")
	before := env.mustGet(t, id)

	resp := env.do(t, http.MethodPatch, "/api/admin/v1/posts/"+id, map[string]any{
		"title": "Live Edited", "status": "draft", // status field is ignored
	})
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch published = %d, want 200", resp.StatusCode)
	}
	post := m["post"].(map[string]any)
	if post["status"] != "published" {
		t.Fatalf("patch published status = %v, want published (status change ignored)", post["status"])
	}
	if post["published_at"] != before["published_at"] {
		t.Fatalf("patch changed published_at %v -> %v", before["published_at"], post["published_at"])
	}
	if post["title"] != "Live Edited" {
		t.Fatalf("patch title = %v, want Live Edited", post["title"])
	}
}

// ---------------------------------------------------------------------------
// Preview token + preview endpoint
// ---------------------------------------------------------------------------

func TestPreviewTokenRoundTrip(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	draft := env.createPost(t, map[string]any{"title": "Secret Draft", "slug": "secret-draft"})
	id := draft["id"].(string)

	// Mint a token via the admin endpoint.
	resp := env.do(t, http.MethodPost, "/api/admin/v1/posts/"+id+"/preview-token", nil)
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview-token = %d, want 200; body = %s", resp.StatusCode, raw)
	}
	token, _ := m["token"].(string)
	if token == "" {
		t.Fatalf("preview-token response has no token: %s", raw)
	}
	if m["expires_at"] == nil {
		t.Fatalf("preview-token response has no expires_at: %s", raw)
	}
	// No template configured → preview_url is null.
	if m["preview_url"] != nil {
		t.Fatalf("preview_url = %v, want null with no template", m["preview_url"])
	}

	// The public preview endpoint serves the draft with no-store.
	resp = env.do(t, http.MethodGet, "/api/v1/preview/posts/secret-draft?token="+url.QueryEscape(token), nil)
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("preview Cache-Control = %q, want no-store", cc)
	}
	_, pm := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview GET = %d, want 200", resp.StatusCode)
	}
	data := dataObj(t, pm)
	if data["slug"] != "secret-draft" || data["title"] != "Secret Draft" {
		t.Fatalf("preview data = %v, want the draft", data)
	}
	// A draft's published_at serializes as null.
	if data["published_at"] != nil {
		t.Fatalf("preview draft published_at = %v, want null", data["published_at"])
	}
}

func TestPreviewTokenRejections(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	a := env.createPost(t, map[string]any{"title": "Post A", "slug": "post-a"})
	b := env.createPost(t, map[string]any{"title": "Post B", "slug": "post-b"})
	idA := a["id"].(string)
	idB := b["id"].(string)

	tk := preview.New(testAppSecret)
	now := time.Now().UTC()
	validA, _ := tk.Mint(idA, now)

	assert401 := func(name, path string) {
		t.Helper()
		resp := env.do(t, http.MethodGet, path, nil)
		_, m := readJSON(t, resp)
		if resp.StatusCode != http.StatusUnauthorized || errorCode(t, m) != "invalid_preview_token" {
			t.Fatalf("%s = %d %s, want 401 invalid_preview_token", name, resp.StatusCode, errorCode(t, m))
		}
	}

	// Missing token.
	assert401("missing token", "/api/v1/preview/posts/post-a")

	// Tampered signature. Flip the first character of the HMAC segment —
	// the last base64url character of a 32-byte MAC only carries 4 data
	// bits, so A↔B there can leave the decoded signature unchanged.
	dot := strings.LastIndexByte(validA, '.')
	sig := []byte(validA[dot+1:])
	if sig[0] == 'A' {
		sig[0] = 'B'
	} else {
		sig[0] = 'A'
	}
	tampered := validA[:dot+1] + string(sig)
	assert401("tampered token", "/api/v1/preview/posts/post-a?token="+url.QueryEscape(tampered))

	// Expired token (minted 20 minutes ago, TTL 15m).
	expired, _ := tk.Mint(idA, now.Add(-20*time.Minute))
	assert401("expired token", "/api/v1/preview/posts/post-a?token="+url.QueryEscape(expired))

	// Token for A used on B's slug → mismatch.
	assert401("cross-post token", "/api/v1/preview/posts/post-b?token="+url.QueryEscape(validA))
	_ = idB

	// Valid token but unknown slug → 404 post_not_found (token was valid).
	validForUnknownSlugPost, _ := tk.Mint("some-id", now)
	resp := env.do(t, http.MethodGet, "/api/v1/preview/posts/no-such-slug?token="+url.QueryEscape(validForUnknownSlugPost), nil)
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, m) != "post_not_found" {
		t.Fatalf("valid token unknown slug = %d %s, want 404 post_not_found", resp.StatusCode, errorCode(t, m))
	}
}

// TestPreviewBypassesAPIDisabled confirms the preview endpoint still works
// when the public content API is disabled, while the normal detail endpoint
// does not.
func TestPreviewBypassesAPIDisabled(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	p := env.createPost(t, map[string]any{"title": "Hidden", "slug": "hidden"})
	id := p["id"].(string)
	env.setPublicAPIEnabled(t, false)

	tk := preview.New(testAppSecret)
	token, _ := tk.Mint(id, time.Now().UTC())

	// Preview works.
	resp := env.do(t, http.MethodGet, "/api/v1/preview/posts/hidden?token="+url.QueryEscape(token), nil)
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview with API disabled = %d, want 200 (bypasses gate)", resp.StatusCode)
	}
	if dataObj(t, m)["slug"] != "hidden" {
		t.Fatalf("preview data.slug = %v, want hidden", dataObj(t, m)["slug"])
	}

	// The normal public detail endpoint is gated off.
	resp = env.do(t, http.MethodGet, "/api/v1/posts/hidden", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, m) != "api_disabled" {
		t.Fatalf("public detail with API disabled = %d %s, want 404 api_disabled", resp.StatusCode, errorCode(t, m))
	}
}

// TestPreviewURLTemplate confirms preview_url is built from the template with
// {token} URL-escaped and {slug} substituted.
func TestPreviewURLTemplate(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	p := env.createPost(t, map[string]any{"title": "Tpl", "slug": "tpl-post"})
	id := p["id"].(string)

	// Configure a template via the settings endpoint.
	resp := env.do(t, http.MethodPatch, "/api/admin/v1/settings", map[string]any{
		"preview_url_template": "https://site.example.com/preview/{slug}?token={token}",
	})
	if resp.StatusCode != http.StatusOK {
		raw, _ := readJSON(t, resp)
		t.Fatalf("set template = %d, want 200; body = %s", resp.StatusCode, raw)
	}
	_ = resp.Body.Close()

	resp = env.do(t, http.MethodPost, "/api/admin/v1/posts/"+id+"/preview-token", nil)
	_, m := readJSON(t, resp)
	token := m["token"].(string)
	got, _ := m["preview_url"].(string)
	want := "https://site.example.com/preview/tpl-post?token=" + url.QueryEscape(token)
	if got != want {
		t.Fatalf("preview_url = %q, want %q", got, want)
	}

	// Clearing the template makes preview_url null again.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/settings", map[string]any{"preview_url_template": nil})
	_ = resp.Body.Close()
	resp = env.do(t, http.MethodPost, "/api/admin/v1/posts/"+id+"/preview-token", nil)
	_, m = readJSON(t, resp)
	if m["preview_url"] != nil {
		t.Fatalf("preview_url after clearing template = %v, want null", m["preview_url"])
	}
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

func TestSettingsGetAndValidation(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	owner := env.ownerID(t)

	// GET: the setup-created settings, with the expected shape.
	resp := env.do(t, http.MethodGet, "/api/admin/v1/settings", nil)
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get settings = %d, want 200; body = %s", resp.StatusCode, raw)
	}
	s := m["settings"].(map[string]any)
	for _, k := range []string{
		"site_name", "site_url", "admin_title", "locale", "available_locales", "timezone",
		"default_author", "cors_origins", "public_api_enabled",
		"preview_url_template", "webhook_user_agent",
	} {
		if _, ok := s[k]; !ok {
			t.Fatalf("settings missing key %q: %s", k, raw)
		}
	}
	if s["default_author"] != nil {
		t.Fatalf("default_author = %v, want null initially", s["default_author"])
	}
	if _, ok := s["cors_origins"].([]any); !ok {
		t.Fatalf("cors_origins = %v, want an array", s["cors_origins"])
	}

	// Valid partial patch: site_name + public_api_enabled + default_author.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/settings", map[string]any{
		"site_name":          "Renamed Blog",
		"public_api_enabled": false,
		"default_author":     owner,
		"cors_origins":       []string{"https://example.com", "https://app.example.com:8443"},
	})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("valid patch = %d, want 200; body = %v", resp.StatusCode, m)
	}
	s = m["settings"].(map[string]any)
	if s["site_name"] != "Renamed Blog" || s["public_api_enabled"] != false || s["default_author"] != owner {
		t.Fatalf("patched settings = %v, want the updated values", s)
	}

	// Clearing default_author with null.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/settings", map[string]any{"default_author": nil})
	_, m = readJSON(t, resp)
	if m["settings"].(map[string]any)["default_author"] != nil {
		t.Fatalf("default_author after null = %v, want null", m["settings"].(map[string]any)["default_author"])
	}

	// Validation matrix.
	cases := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"empty site_name", map[string]any{"site_name": "  "}, "site_name"},
		{"bad site_url", map[string]any{"site_url": "notaurl"}, "site_url"},
		{"bad timezone", map[string]any{"timezone": "Mars/Phobos"}, "timezone"},
		{"cors with path", map[string]any{"cors_origins": []string{"https://example.com/app"}}, "cors_origins"},
		{"cors with query", map[string]any{"cors_origins": []string{"https://example.com?x=1"}}, "cors_origins"},
		{"unknown default_author", map[string]any{"default_author": "no-such-user"}, "default_author"},
		{"template without token", map[string]any{"preview_url_template": "https://x.example.com/p/{slug}"}, "preview_url_template"},
		{"template not http", map[string]any{"preview_url_template": "ftp://x/{token}"}, "preview_url_template"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := env.do(t, http.MethodPatch, "/api/admin/v1/settings", tc.body)
			_, m := readJSON(t, resp)
			if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "validation_failed" {
				t.Fatalf("%s = %d %s, want 400 validation_failed", tc.name, resp.StatusCode, errorCode(t, m))
			}
			if _, ok := validationDetails(t, m)[tc.field]; !ok {
				t.Fatalf("%s: details missing %q: %v", tc.name, tc.field, m)
			}
		})
	}
}

// TestSettingsPermissions confirms GET settings is readable by any
// authenticated user (editors need available_locales), PATCH remains
// owner-only, and anon requests are 401.
func TestSettingsPermissions(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	editor := env.newEditorClient(t, "editor@example.com")

	// Editor may GET settings (for available_locales in the post editor).
	resp := env.doWith(t, editor, http.MethodGet, "/api/admin/v1/settings", nil)
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET settings as editor = %d %v, want 200", resp.StatusCode, m)
	}
	if _, ok := m["settings"]; !ok {
		t.Fatalf("GET settings as editor missing settings key: %v", m)
	}

	// Editor may not PATCH.
	resp = env.doWith(t, editor, http.MethodPatch, "/api/admin/v1/settings", map[string]any{"site_name": "Nope"})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusForbidden || errorCode(t, m) != "forbidden" {
		t.Fatalf("PATCH settings as editor = %d %s, want 403 forbidden", resp.StatusCode, errorCode(t, m))
	}

	// Anon → 401 for both.
	for _, tc := range []struct {
		method string
		body   any
	}{
		{http.MethodGet, nil},
		{http.MethodPatch, map[string]any{"site_name": "Nope"}},
	} {
		resp = env.doWith(t, &http.Client{}, tc.method, "/api/admin/v1/settings", tc.body)
		_, m = readJSON(t, resp)
		if resp.StatusCode != http.StatusUnauthorized || errorCode(t, m) != "unauthorized" {
			t.Fatalf("%s settings anon = %d %s, want 401 unauthorized", tc.method, resp.StatusCode, errorCode(t, m))
		}
	}
}
