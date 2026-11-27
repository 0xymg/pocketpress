package http

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/media"
	"github.com/pocketpress/pocketpress/internal/store"
)

// newTestEnvWithCORS is newTestEnv (see adminapi_test.go) plus a static
// PP_CORS_ORIGINS-equivalent allowlist, for the one test that needs to
// confirm the static config and the runtime settings.cors_origins
// allowlist are unioned rather than one replacing the other.
func newTestEnvWithCORS(t *testing.T, corsOrigins []string) *testEnv {
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
		Logger:      discardLogger(),
		Version:     "test",
		DB:          st.DB(),
		MediaDriver: mediaDriver,
		DataDir:     dataDir,
		MaxUploadMB: 10,
		CORSOrigins: corsOrigins,
		AppSecret:   testAppSecret,
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

// ---------------------------------------------------------------------------
// testEnv helpers (build on the testEnv defined in adminapi_test.go)
// ---------------------------------------------------------------------------

// setPostState sets a post's status and published_at directly through the
// store. There is no admin "publish" operation yet (Phase 6), so
// integration tests seed publish/scheduled state this way, per the
// development plan's Phase 5 instructions.
func (e *testEnv) setPostState(t *testing.T, postID, status string, publishedAt *time.Time) {
	t.Helper()
	var pubArg any
	if publishedAt != nil {
		pubArg = publishedAt.UTC().Format(time.RFC3339)
	}
	if _, err := e.db.ExecContext(context.Background(),
		`UPDATE posts SET status = ?, published_at = ? WHERE id = ?`, status, pubArg, postID,
	); err != nil {
		t.Fatalf("setPostState(%s, %s): %v", postID, status, err)
	}
}

// publishPost marks a post published with the given published_at.
func (e *testEnv) publishPost(t *testing.T, postID string, publishedAt time.Time) {
	t.Helper()
	e.setPostState(t, postID, store.PostStatusPublished, &publishedAt)
}

// setPostUpdatedAt backdates a post's updated_at directly through the
// store. updated_at (and so the detail endpoint's ETag, which is derived
// from it) is stored with whole-second precision (RFC3339, no fractional
// seconds) — two writes within the same wall-clock second produce an
// identical stored value. Tests that need a guaranteed-different ETag
// after a later edit backdate the "before" timestamp with this helper
// rather than relying on real-time drift between two fast HTTP calls.
func (e *testEnv) setPostUpdatedAt(t *testing.T, postID string, at time.Time) {
	t.Helper()
	if _, err := e.db.ExecContext(context.Background(),
		`UPDATE posts SET updated_at = ? WHERE id = ?`, at.UTC().Format(time.RFC3339), postID,
	); err != nil {
		t.Fatalf("setPostUpdatedAt(%s): %v", postID, err)
	}
}

// setPublicAPIEnabled flips settings.public_api_enabled directly through
// the store — there is no admin settings endpoint yet.
func (e *testEnv) setPublicAPIEnabled(t *testing.T, enabled bool) {
	t.Helper()
	s, err := store.GetSettings(context.Background(), e.db)
	if err != nil {
		t.Fatalf("GetSettings(): %v", err)
	}
	s.PublicAPIEnabled = enabled
	if err := store.UpdateSettings(context.Background(), e.db, s); err != nil {
		t.Fatalf("UpdateSettings(): %v", err)
	}
}

// setCORSOrigins sets settings.cors_origins directly through the store.
func (e *testEnv) setCORSOrigins(t *testing.T, origins []string) {
	t.Helper()
	s, err := store.GetSettings(context.Background(), e.db)
	if err != nil {
		t.Fatalf("GetSettings(): %v", err)
	}
	s.CORSOrigins = origins
	if err := store.UpdateSettings(context.Background(), e.db, s); err != nil {
		t.Fatalf("UpdateSettings(): %v", err)
	}
}

// ownerID returns the setup-created owner's user id.
func (e *testEnv) ownerID(t *testing.T) string {
	t.Helper()
	u, err := store.GetUserByEmail(context.Background(), e.db, "owner@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail(owner): %v", err)
	}
	return u.ID
}

// dataObj extracts the top-level "data" object from a decoded envelope.
func dataObj(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	d, ok := m["data"].(map[string]any)
	if !ok {
		t.Fatalf("body has no data object: %v", m)
	}
	return d
}

// dataArr extracts the top-level "data" array from a decoded envelope.
func dataArr(t *testing.T, m map[string]any) []any {
	t.Helper()
	d, ok := m["data"].([]any)
	if !ok {
		t.Fatalf("body has no data array: %v", m)
	}
	return d
}

// metaObj extracts the top-level "meta" object from a decoded envelope.
func metaObj(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	d, ok := m["meta"].(map[string]any)
	if !ok {
		t.Fatalf("body has no meta object: %v", m)
	}
	return d
}

// slugsOf extracts the "slug" field of every element of a public post list.
func slugsOf(t *testing.T, arr []any) []string {
	t.Helper()
	out := make([]string, len(arr))
	for i, v := range arr {
		obj, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("post list element is not an object: %v", v)
		}
		out[i], _ = obj["slug"].(string)
	}
	return out
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Visibility
// ---------------------------------------------------------------------------

// TestPublicPostVisibility confirms the public API's hard visibility rule:
// only status=published AND published_at set AND published_at <= now is
// ever visible, in both the list and detail endpoints. Draft, archived,
// never-scheduled and scheduled-but-not-yet-status-published posts are all
// invisible, including a post whose published_at happens to be in the past
// while its status is still "scheduled" (not published).
func TestPublicPostVisibility(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	now := time.Now().UTC()

	draft := env.createPost(t, map[string]any{"title": "Draft Post", "slug": "draft-post"})
	archived := env.createPost(t, map[string]any{"title": "Archived Post", "slug": "archived-post", "status": "archived"})
	scheduledFuture := env.createPost(t, map[string]any{"title": "Scheduled Future", "slug": "scheduled-future"})
	env.setPostState(t, scheduledFuture["id"].(string), store.PostStatusScheduled, timePtr(now.Add(time.Hour)))
	scheduledPastButNotPublished := env.createPost(t, map[string]any{"title": "Scheduled Past", "slug": "scheduled-past"})
	env.setPostState(t, scheduledPastButNotPublished["id"].(string), store.PostStatusScheduled, timePtr(now.Add(-time.Hour)))
	published := env.createPost(t, map[string]any{"title": "Published Post", "slug": "published-post"})
	env.publishPost(t, published["id"].(string), now.Add(-time.Hour))

	hidden := []map[string]any{draft, archived, scheduledFuture, scheduledPastButNotPublished}
	for _, p := range hidden {
		slug := p["slug"].(string)
		resp := env.do(t, http.MethodGet, "/api/v1/posts/"+slug, nil)
		_, m := readJSON(t, resp)
		if resp.StatusCode != http.StatusNotFound || errorCode(t, m) != "post_not_found" {
			t.Fatalf("GET /posts/%s = %d %s, want 404 post_not_found", slug, resp.StatusCode, errorCode(t, m))
		}
	}

	resp := env.do(t, http.MethodGet, "/api/v1/posts", nil)
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /posts status = %d, want 200", resp.StatusCode)
	}
	slugs := slugsOf(t, dataArr(t, m))
	if len(slugs) != 1 || slugs[0] != "published-post" {
		t.Fatalf("GET /posts slugs = %v, want only [published-post]", slugs)
	}
	if meta := metaObj(t, m); meta["total_items"] != float64(1) {
		t.Fatalf("GET /posts meta.total_items = %v, want 1", meta["total_items"])
	}

	resp = env.do(t, http.MethodGet, "/api/v1/posts/published-post", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /posts/published-post status = %d, want 200", resp.StatusCode)
	}
	if dataObj(t, m)["slug"] != "published-post" {
		t.Fatalf("GET /posts/published-post data.slug = %v, want published-post", dataObj(t, m)["slug"])
	}
}

func timePtr(t time.Time) *time.Time { return &t }

// ---------------------------------------------------------------------------
// Pagination
// ---------------------------------------------------------------------------

// TestPublicPostsPaginationMathAndPerPageCap covers default page/per_page,
// the per_page cap at 100, and total_pages/total_items math.
func TestPublicPostsPaginationMathAndPerPageCap(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	base := time.Now().UTC().Add(-time.Hour)

	const total = 25
	for i := 0; i < total; i++ {
		slug := "post-" + string(rune('a'+i))
		p := env.createPost(t, map[string]any{"title": "Post", "slug": slug})
		env.publishPost(t, p["id"].(string), base.Add(time.Duration(i)*time.Minute))
	}

	// Default page/per_page.
	resp := env.do(t, http.MethodGet, "/api/v1/posts", nil)
	_, m := readJSON(t, resp)
	meta := metaObj(t, m)
	if meta["page"] != float64(1) || meta["per_page"] != float64(20) {
		t.Fatalf("default meta = %v, want page 1 per_page 20", meta)
	}
	if meta["total_items"] != float64(total) || meta["total_pages"] != float64(2) {
		t.Fatalf("default meta totals = %v, want total_items 25 total_pages 2", meta)
	}
	if len(dataArr(t, m)) != 20 {
		t.Fatalf("default page len = %d, want 20", len(dataArr(t, m)))
	}

	// per_page=10, page=3 -> last 5 items.
	resp = env.do(t, http.MethodGet, "/api/v1/posts?per_page=10&page=3", nil)
	_, m = readJSON(t, resp)
	meta = metaObj(t, m)
	if meta["total_pages"] != float64(3) {
		t.Fatalf("per_page=10 total_pages = %v, want 3", meta["total_pages"])
	}
	if len(dataArr(t, m)) != 5 {
		t.Fatalf("page 3 len = %d, want 5", len(dataArr(t, m)))
	}

	// per_page over the cap clamps to 100 (still only 25 rows to return).
	resp = env.do(t, http.MethodGet, "/api/v1/posts?per_page=500", nil)
	_, m = readJSON(t, resp)
	meta = metaObj(t, m)
	if meta["per_page"] != float64(100) {
		t.Fatalf("per_page=500 meta.per_page = %v, want clamped to 100", meta["per_page"])
	}
	if len(dataArr(t, m)) != total {
		t.Fatalf("per_page=500 len = %d, want %d", len(dataArr(t, m)), total)
	}

	// Invalid page/per_page fall back to defaults.
	resp = env.do(t, http.MethodGet, "/api/v1/posts?page=0&per_page=abc", nil)
	_, m = readJSON(t, resp)
	meta = metaObj(t, m)
	if meta["page"] != float64(1) || meta["per_page"] != float64(20) {
		t.Fatalf("invalid page/per_page meta = %v, want page 1 per_page 20", meta)
	}
}

// ---------------------------------------------------------------------------
// Filters
// ---------------------------------------------------------------------------

// TestPublicPostFilters covers category, tag, featured and author filters
// on GET /api/v1/posts, plus the category/tag-scoped post-list endpoints
// and their 404s for an unknown slug.
func TestPublicPostFilters(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	owner := env.ownerID(t)
	editorClient := env.newEditorClient(t, "editor@example.com")

	catID := env.createCategory(t, "News")
	catSlug := "news"
	tagID := env.createTag(t, "Go")
	tagSlug := "go"

	now := time.Now().UTC()

	// P1: in News, tagged Go, featured, authored by owner.
	p1 := env.createPost(t, map[string]any{
		"title": "P1", "slug": "p1", "categories": []string{catID}, "tags": []string{tagID}, "featured": true,
	})
	env.publishPost(t, p1["id"].(string), now.Add(-4*time.Minute))

	// P2: no category/tag, not featured, authored by owner.
	p2 := env.createPost(t, map[string]any{"title": "P2", "slug": "p2", "featured": false})
	env.publishPost(t, p2["id"].(string), now.Add(-3*time.Minute))

	// P3: in News, not tagged, authored by the editor.
	resp := env.doWith(t, editorClient, http.MethodPost, "/api/admin/v1/posts", map[string]any{
		"title": "P3", "slug": "p3", "categories": []string{catID},
	})
	_, pm := readJSON(t, resp)
	p3 := pm["post"].(map[string]any)
	env.publishPost(t, p3["id"].(string), now.Add(-2*time.Minute))

	editor, err := store.GetUserByEmail(context.Background(), env.db, "editor@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail(editor): %v", err)
	}

	assertSlugs := func(name, path string, want ...string) {
		t.Helper()
		resp := env.do(t, http.MethodGet, path, nil)
		raw, m := readJSON(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200; body = %s", name, resp.StatusCode, raw)
		}
		got := slugsOf(t, dataArr(t, m))
		if len(got) != len(want) {
			t.Fatalf("%s: slugs = %v, want %v", name, got, want)
		}
		for _, w := range want {
			if !containsString(got, w) {
				t.Fatalf("%s: slugs = %v, want to contain %q", name, got, w)
			}
		}
	}

	assertSlugs("category=news", "/api/v1/posts?category="+catSlug, "p1", "p3")
	assertSlugs("tag=go", "/api/v1/posts?tag="+tagSlug, "p1")
	assertSlugs("featured=true", "/api/v1/posts?featured=true", "p1")
	assertSlugs("featured=false", "/api/v1/posts?featured=false", "p2", "p3")
	assertSlugs("author=owner", "/api/v1/posts?author="+owner, "p1", "p2")
	assertSlugs("author=editor", "/api/v1/posts?author="+editor.ID, "p3")
	assertSlugs("category=unknown-slug", "/api/v1/posts?category=unknown-slug")

	assertSlugs("categories/{slug}/posts", "/api/v1/categories/"+catSlug+"/posts", "p1", "p3")
	assertSlugs("tags/{slug}/posts", "/api/v1/tags/"+tagSlug+"/posts", "p1")
	assertSlugs("categories/{slug}/posts?tag=go", "/api/v1/categories/"+catSlug+"/posts?tag="+tagSlug, "p1")

	// Unknown category/tag slug in the scoped endpoints is a 404, not an
	// empty list — the slug in the path names the resource itself.
	resp = env.do(t, http.MethodGet, "/api/v1/categories/unknown-slug/posts", nil)
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, m) != "category_not_found" {
		t.Fatalf("categories/unknown-slug/posts = %d %s, want 404 category_not_found", resp.StatusCode, errorCode(t, m))
	}
	resp = env.do(t, http.MethodGet, "/api/v1/tags/unknown-slug/posts", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, m) != "tag_not_found" {
		t.Fatalf("tags/unknown-slug/posts = %d %s, want 404 tag_not_found", resp.StatusCode, errorCode(t, m))
	}
}

// TestPublicPostSortVariants covers all four documented sort values plus
// the default.
func TestPublicPostSortVariants(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	now := time.Now().UTC()

	// Titles and publish order deliberately don't match, so a sort visibly
	// reorders the list.
	c := env.createPost(t, map[string]any{"title": "Charlie", "slug": "charlie"})
	env.publishPost(t, c["id"].(string), now.Add(-3*time.Minute))
	a := env.createPost(t, map[string]any{"title": "alpha", "slug": "alpha"})
	env.publishPost(t, a["id"].(string), now.Add(-2*time.Minute))
	b := env.createPost(t, map[string]any{"title": "Bravo", "slug": "bravo"})
	env.publishPost(t, b["id"].(string), now.Add(-1*time.Minute))

	assertOrder := func(name, path string, want ...string) {
		t.Helper()
		resp := env.do(t, http.MethodGet, path, nil)
		_, m := readJSON(t, resp)
		got := slugsOf(t, dataArr(t, m))
		if len(got) != len(want) {
			t.Fatalf("%s: slugs = %v, want %v", name, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: slugs = %v, want %v", name, got, want)
			}
		}
	}

	assertOrder("default (-published_at)", "/api/v1/posts", "bravo", "alpha", "charlie")
	assertOrder("-published_at", "/api/v1/posts?sort=-published_at", "bravo", "alpha", "charlie")
	assertOrder("published_at", "/api/v1/posts?sort=published_at", "charlie", "alpha", "bravo")
	assertOrder("title", "/api/v1/posts?sort=title", "alpha", "bravo", "charlie")
	assertOrder("-title", "/api/v1/posts?sort=-title", "charlie", "bravo", "alpha")
}

// TestPublicPostFullTextSearch covers GET /api/v1/posts?q= — FTS over
// title/excerpt/body, draft exclusion, and punctuation-only empty results.
func TestPublicPostFullTextSearch(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	now := time.Now().UTC()

	guide := env.createPost(t, map[string]any{
		"title": "Alpha Guide", "slug": "alpha-guide",
		"content_html": "<p>full text search rocks in production</p>",
	})
	env.publishPost(t, guide["id"].(string), now.Add(-2*time.Minute))

	tips := env.createPost(t, map[string]any{
		"title": "Search Tips", "slug": "search-tips",
		"content_html": "<p>alpha techniques for finding content</p>",
	})
	env.publishPost(t, tips["id"].(string), now.Add(-time.Minute))

	draft := env.createPost(t, map[string]any{
		"title": "Hidden Draft", "slug": "hidden-draft",
		"content_html": "<p>production secrets</p>",
	})
	_ = draft

	assertSlugs := func(name, path string, want ...string) {
		t.Helper()
		resp := env.do(t, http.MethodGet, path, nil)
		raw, m := readJSON(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200; body = %s", name, resp.StatusCode, raw)
		}
		got := slugsOf(t, dataArr(t, m))
		if len(got) != len(want) {
			t.Fatalf("%s: slugs = %v, want %v", name, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: slugs = %v, want %v", name, got, want)
			}
		}
	}

	// Body match; draft with the same term must not appear.
	assertSlugs("q=production", "/api/v1/posts?q=production", "alpha-guide")
	// Title match ranks above body match for "search".
	assertSlugs("q=search", "/api/v1/posts?q=search", "search-tips", "alpha-guide")
	assertSlugs("q=alph", "/api/v1/posts?q=alph", "alpha-guide", "search-tips")
	assertSlugs("punctuation", "/api/v1/posts?q=%22+*+OR")
}

// ---------------------------------------------------------------------------
// ETag / caching
// ---------------------------------------------------------------------------

// TestPublicPostDetailETagAndConditionalGet covers the detail endpoint's
// ETag, If-None-Match -> 304 handling, and that the ETag changes (and a
// stale If-None-Match yields 200, not 304) once the post is modified.
func TestPublicPostDetailETagAndConditionalGet(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	now := time.Now().UTC()

	p := env.createPost(t, map[string]any{"title": "ETag Post", "slug": "etag-post"})
	env.publishPost(t, p["id"].(string), now.Add(-time.Minute))
	// Backdate updated_at so the later PATCH's real-time write is guaranteed
	// to land in a different whole second (see setPostUpdatedAt).
	env.setPostUpdatedAt(t, p["id"].(string), now.Add(-time.Hour))

	resp := env.do(t, http.MethodGet, "/api/v1/posts/etag-post", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initial GET status = %d, want 200", resp.StatusCode)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("initial GET has no ETag header")
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=0, must-revalidate" {
		t.Fatalf("initial GET Cache-Control = %q, want %q", cc, "public, max-age=0, must-revalidate")
	}
	_, _ = readJSON(t, resp)

	req, _ := http.NewRequest(http.MethodGet, env.srv.URL+"/api/v1/posts/etag-post", nil)
	req.Header.Set("If-None-Match", etag)
	resp, err := env.client.Do(req)
	if err != nil {
		t.Fatalf("conditional GET: %v", err)
	}
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("conditional GET status = %d, want 304", resp.StatusCode)
	}
	if got := resp.Header.Get("ETag"); got != etag {
		t.Fatalf("304 response ETag = %q, want %q", got, etag)
	}
	body, _ := readJSON(t, resp)
	if len(body) != 0 {
		t.Fatalf("304 response body = %q, want empty", body)
	}

	// Modify the post (bumps updated_at) via the admin API; the ETag must
	// change, and the old If-None-Match value must no longer match (200).
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/posts/"+p["id"].(string), map[string]any{"title": "ETag Post Edited"})
	if resp.StatusCode != http.StatusOK {
		raw, _ := readJSON(t, resp)
		t.Fatalf("patch post status = %d, want 200; body = %s", resp.StatusCode, raw)
	}
	_, _ = readJSON(t, resp)

	req, _ = http.NewRequest(http.MethodGet, env.srv.URL+"/api/v1/posts/etag-post", nil)
	req.Header.Set("If-None-Match", etag)
	resp, err = env.client.Do(req)
	if err != nil {
		t.Fatalf("conditional GET after edit: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("conditional GET after edit status = %d, want 200 (stale ETag)", resp.StatusCode)
	}
	newETag := resp.Header.Get("ETag")
	if newETag == "" || newETag == etag {
		t.Fatalf("new ETag = %q, want a non-empty value different from %q", newETag, etag)
	}
	_, m := readJSON(t, resp)
	if dataObj(t, m)["title"] != "ETag Post Edited" {
		t.Fatalf("data.title = %v, want the edited title", dataObj(t, m)["title"])
	}
}

// TestPublicPostsListCacheControl confirms list endpoints carry the
// documented 60s Cache-Control.
func TestPublicPostsListCacheControl(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	resp := env.do(t, http.MethodGet, "/api/v1/posts", nil)
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=60" {
		t.Fatalf("GET /posts Cache-Control = %q, want %q", cc, "public, max-age=60")
	}
	if ct := resp.Header.Get("X-Content-Type-Options"); ct != "nosniff" {
		t.Fatalf("GET /posts X-Content-Type-Options = %q, want nosniff", ct)
	}
	_, _ = readJSON(t, resp)
}

// ---------------------------------------------------------------------------
// Serializer shape
// ---------------------------------------------------------------------------

// TestPublicPostSerializerShape asserts the exact top-level JSON keys of a
// public post (no admin-only leakage: status, content_json, content_text,
// scheduled_for, ...), cover_image null vs. populated, and seo nulls for
// unset fields.
func TestPublicPostSerializerShape(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	now := time.Now().UTC()

	cover := env.createMedia(t, "cover.png", pngBytes(t))
	catID := env.createCategory(t, "News")
	tagID := env.createTag(t, "Go")

	full := env.createPost(t, map[string]any{
		"title": "Full Post", "slug": "full-post",
		"excerpt": "An excerpt.", "content_html": "<p>Body</p>",
		"cover_image": cover["id"], "categories": []string{catID}, "tags": []string{tagID},
		"featured": true, "seo_title": "SEO Title", "seo_description": "SEO description.",
		"canonical_url": "https://example.com/full-post", "og_image": cover["id"],
	})
	env.publishPost(t, full["id"].(string), now.Add(-time.Minute))

	bare := env.createPost(t, map[string]any{"title": "Bare Post", "slug": "bare-post"})
	env.publishPost(t, bare["id"].(string), now.Add(-2*time.Minute))

	// Full post: every key present, no leakage, populated cover_image/seo.
	resp := env.do(t, http.MethodGet, "/api/v1/posts/full-post", nil)
	raw, m := readJSON(t, resp)
	data := dataObj(t, m)

	wantKeys := []string{
		"id", "title", "slug", "excerpt", "content_html", "cover_image", "author",
		"categories", "tags", "featured", "locale", "translations", "seo", "published_at", "updated_at",
	}
	for _, k := range wantKeys {
		if _, ok := data[k]; !ok {
			t.Fatalf("data missing key %q: %s", k, raw)
		}
	}
	if len(data) != len(wantKeys) {
		t.Fatalf("data has %d keys, want %d (data = %v)", len(data), len(wantKeys), data)
	}
	for _, leaked := range []string{"status", "content_json", "content_text", "scheduled_for", "created_at"} {
		if _, ok := data[leaked]; ok {
			t.Fatalf("data leaks internal field %q: %s", leaked, raw)
		}
	}

	author, ok := data["author"].(map[string]any)
	if !ok {
		t.Fatalf("data.author is not an object: %s", raw)
	}
	for _, k := range []string{"id", "name", "slug", "bio", "website", "location", "avatar"} {
		if _, ok := author[k]; !ok {
			t.Fatalf("data.author missing %q: %v", k, author)
		}
	}
	if len(author) != 7 {
		t.Fatalf("data.author = %v, want exactly {id,name,slug,bio,website,location,avatar}", author)
	}
	if author["slug"] == nil || author["slug"] == "" {
		t.Fatalf("data.author.slug empty: %v", author)
	}
	if _, ok := author["email"]; ok {
		t.Fatalf("data.author leaks email: %s", raw)
	}

	coverImage, ok := data["cover_image"].(map[string]any)
	if !ok {
		t.Fatalf("data.cover_image is not an object: %s", raw)
	}
	url, _ := coverImage["url"].(string)
	if url == "" || url[0] != '/' {
		t.Fatalf("data.cover_image.url = %q, want a relative /api/files/... path", url)
	}

	seo, ok := data["seo"].(map[string]any)
	if !ok {
		t.Fatalf("data.seo is not an object: %s", raw)
	}
	if seo["title"] != "SEO Title" || seo["description"] != "SEO description." {
		t.Fatalf("data.seo = %v, want the populated title/description", seo)
	}
	if seo["canonical_url"] != "https://example.com/full-post" {
		t.Fatalf("data.seo.canonical_url = %v, want the set value", seo["canonical_url"])
	}
	if ogURL, _ := seo["og_image_url"].(string); ogURL == "" {
		t.Fatalf("data.seo.og_image_url = %v, want a populated URL", seo["og_image_url"])
	}

	cats, _ := data["categories"].([]any)
	if len(cats) != 1 {
		t.Fatalf("data.categories = %v, want one entry", data["categories"])
	}
	catObj := cats[0].(map[string]any)
	if len(catObj) != 3 || catObj["id"] == nil || catObj["name"] == nil || catObj["slug"] == nil {
		t.Fatalf("category ref = %v, want exactly {id,name,slug}", catObj)
	}

	// Bare post: cover_image null, seo fields all null.
	resp = env.do(t, http.MethodGet, "/api/v1/posts/bare-post", nil)
	raw, m = readJSON(t, resp)
	data = dataObj(t, m)
	if data["cover_image"] != nil {
		t.Fatalf("bare post data.cover_image = %v, want null: %s", data["cover_image"], raw)
	}
	seo, ok = data["seo"].(map[string]any)
	if !ok {
		t.Fatalf("bare post data.seo is not an object: %s", raw)
	}
	for _, k := range []string{"title", "description", "canonical_url", "og_image_url"} {
		if seo[k] != nil {
			t.Fatalf("bare post data.seo.%s = %v, want null", k, seo[k])
		}
	}
	if cats, _ := data["categories"].([]any); len(cats) != 0 {
		t.Fatalf("bare post data.categories = %v, want empty", data["categories"])
	}
}

// ---------------------------------------------------------------------------
// Categories / tags
// ---------------------------------------------------------------------------

// TestPublicCategoriesAndTags covers the list/detail shapes and the
// category_not_found/tag_not_found 404 codes.
func TestPublicCategoriesAndTags(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	env.createCategory(t, "News")
	env.createTag(t, "Go")

	resp := env.do(t, http.MethodGet, "/api/v1/categories", nil)
	_, m := readJSON(t, resp)
	cats := dataArr(t, m)
	if len(cats) != 1 {
		t.Fatalf("categories list = %v, want one entry", cats)
	}
	catObj := cats[0].(map[string]any)
	if catObj["slug"] != "news" || catObj["name"] != "News" {
		t.Fatalf("category = %v, want News/news", catObj)
	}
	if _, ok := catObj["description"]; !ok {
		t.Fatalf("category missing description key: %v", catObj)
	}

	resp = env.do(t, http.MethodGet, "/api/v1/categories/news", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK || dataObj(t, m)["slug"] != "news" {
		t.Fatalf("GET /categories/news = %d %v, want 200 slug=news", resp.StatusCode, m)
	}

	resp = env.do(t, http.MethodGet, "/api/v1/categories/no-such-category", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, m) != "category_not_found" {
		t.Fatalf("GET /categories/no-such-category = %d %s, want 404 category_not_found", resp.StatusCode, errorCode(t, m))
	}

	resp = env.do(t, http.MethodGet, "/api/v1/tags", nil)
	_, m = readJSON(t, resp)
	tags := dataArr(t, m)
	if len(tags) != 1 {
		t.Fatalf("tags list = %v, want one entry", tags)
	}
	tagObj := tags[0].(map[string]any)
	if len(tagObj) != 3 || tagObj["slug"] != "go" {
		t.Fatalf("tag = %v, want exactly {id,name,slug} with slug=go", tagObj)
	}

	resp = env.do(t, http.MethodGet, "/api/v1/tags/go", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK || dataObj(t, m)["slug"] != "go" {
		t.Fatalf("GET /tags/go = %d %v, want 200 slug=go", resp.StatusCode, m)
	}

	resp = env.do(t, http.MethodGet, "/api/v1/tags/no-such-tag", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, m) != "tag_not_found" {
		t.Fatalf("GET /tags/no-such-tag = %d %s, want 404 tag_not_found", resp.StatusCode, errorCode(t, m))
	}
}

// ---------------------------------------------------------------------------
// api_disabled
// ---------------------------------------------------------------------------

// TestPublicAPIDisabled confirms every /api/v1/* route 404s with the
// api_disabled envelope when settings.public_api_enabled is false, and
// that the admin API and file serving are unaffected.
func TestPublicAPIDisabled(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	env.setPublicAPIEnabled(t, false)

	paths := []string{
		"/api/v1/posts",
		"/api/v1/posts/whatever",
		"/api/v1/categories",
		"/api/v1/categories/whatever",
		"/api/v1/categories/whatever/posts",
		"/api/v1/tags",
		"/api/v1/tags/whatever",
		"/api/v1/tags/whatever/posts",
		"/api/v1/authors",
		"/api/v1/authors/whatever",
		"/api/v1/authors/whatever/posts",
		"/api/v1/feed.rss",
		"/api/v1/feed.atom",
		"/api/v1/sitemap.xml",
	}
	for _, path := range paths {
		resp := env.do(t, http.MethodGet, path, nil)
		_, m := readJSON(t, resp)
		if resp.StatusCode != http.StatusNotFound || errorCode(t, m) != "api_disabled" {
			t.Fatalf("GET %s (disabled) = %d %s, want 404 api_disabled", path, resp.StatusCode, errorCode(t, m))
		}
		errObj := m["error"].(map[string]any)
		if errObj["message"] != "The public API is disabled." {
			t.Fatalf("GET %s message = %v, want the frozen api_disabled message", path, errObj["message"])
		}
	}

	// The admin API itself is unaffected.
	resp := env.do(t, http.MethodGet, "/api/admin/v1/auth/me", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin /auth/me with public API disabled = %d, want 200", resp.StatusCode)
	}
	_, _ = readJSON(t, resp)

	// Re-enabling restores service.
	env.setPublicAPIEnabled(t, true)
	resp = env.do(t, http.MethodGet, "/api/v1/posts", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /posts after re-enabling = %d, want 200", resp.StatusCode)
	}
	_, _ = readJSON(t, resp)
}

// ---------------------------------------------------------------------------
// CORS
// ---------------------------------------------------------------------------

// TestPublicAPICORSAllowedOrigin confirms an Origin matching
// settings.cors_origins gets the CORS response headers.
func TestPublicAPICORSAllowedOrigin(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	env.setCORSOrigins(t, []string{"https://allowed.example.com"})

	req, _ := http.NewRequest(http.MethodGet, env.srv.URL+"/api/v1/posts", nil)
	req.Header.Set("Origin", "https://allowed.example.com")
	resp, err := env.client.Do(req)
	if err != nil {
		t.Fatalf("GET /posts with Origin: %v", err)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://allowed.example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want the matching origin echoed back", got)
	}
	if got := resp.Header.Get("Vary"); got != "Origin" {
		t.Fatalf("Vary = %q, want Origin", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Methods"); got != "GET, HEAD, OPTIONS" {
		t.Fatalf("Access-Control-Allow-Methods = %q, want GET, HEAD, OPTIONS", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want unset", got)
	}
	_, _ = readJSON(t, resp)
}

// TestPublicAPICORSDisallowedOrigin confirms a non-matching Origin gets no
// CORS headers, while the request is still served.
func TestPublicAPICORSDisallowedOrigin(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	env.setCORSOrigins(t, []string{"https://allowed.example.com"})

	req, _ := http.NewRequest(http.MethodGet, env.srv.URL+"/api/v1/posts", nil)
	req.Header.Set("Origin", "https://evil.example.net")
	resp, err := env.client.Do(req)
	if err != nil {
		t.Fatalf("GET /posts with disallowed Origin: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (still served same-origin)", resp.StatusCode)
	}
	for _, h := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Methods"} {
		if got := resp.Header.Get(h); got != "" {
			t.Fatalf("%s = %q, want unset for a disallowed origin", h, got)
		}
	}
	_, _ = readJSON(t, resp)
}

// TestPublicAPICORSPreflight confirms an OPTIONS preflight to a public API
// path returns a bare 204.
func TestPublicAPICORSPreflight(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	env.setCORSOrigins(t, []string{"https://allowed.example.com"})

	req, _ := http.NewRequest(http.MethodOptions, env.srv.URL+"/api/v1/posts", nil)
	req.Header.Set("Origin", "https://allowed.example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	resp, err := env.client.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS /posts: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://allowed.example.com" {
		t.Fatalf("preflight Access-Control-Allow-Origin = %q, want the matching origin", got)
	}
}

// TestPublicAPICORSEnvOriginAllowed confirms an origin from the static
// PP_CORS_ORIGINS configuration (not settings.cors_origins) is honored too
// — the two allowlists are a union.
func TestPublicAPICORSEnvOriginAllowed(t *testing.T) {
	env := newTestEnvWithCORS(t, []string{"https://static.example.com"})
	env.setup(t)

	req, _ := http.NewRequest(http.MethodGet, env.srv.URL+"/api/v1/posts", nil)
	req.Header.Set("Origin", "https://static.example.com")
	resp, err := env.client.Do(req)
	if err != nil {
		t.Fatalf("GET /posts with static-config Origin: %v", err)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://static.example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want the statically-configured origin", got)
	}
	_, _ = readJSON(t, resp)
}
