package http

import (
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers (build on the testEnv defined in adminapi_test.go)
// ---------------------------------------------------------------------------

func (e *testEnv) createCategory(t *testing.T, name string) string {
	t.Helper()
	resp := e.do(t, http.MethodPost, "/api/admin/v1/categories", map[string]any{"name": name})
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create category %q status = %d; body = %s", name, resp.StatusCode, raw)
	}
	id, _ := m["category"].(map[string]any)["id"].(string)
	if id == "" {
		t.Fatalf("created category has no id: %s", raw)
	}
	return id
}

func (e *testEnv) createTag(t *testing.T, name string) string {
	t.Helper()
	resp := e.do(t, http.MethodPost, "/api/admin/v1/tags", map[string]any{"name": name})
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create tag %q status = %d; body = %s", name, resp.StatusCode, raw)
	}
	id, _ := m["tag"].(map[string]any)["id"].(string)
	if id == "" {
		t.Fatalf("created tag has no id: %s", raw)
	}
	return id
}

func (e *testEnv) createPost(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	resp := e.do(t, http.MethodPost, "/api/admin/v1/posts", body)
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create post status = %d; body = %s", resp.StatusCode, raw)
	}
	post, _ := m["post"].(map[string]any)
	if post == nil {
		t.Fatalf("create post response has no post: %s", raw)
	}
	return post
}

// newEditorClient creates an editor account and returns a cookie-jar client
// already logged in as that editor.
func (e *testEnv) newEditorClient(t *testing.T, email string) *http.Client {
	t.Helper()
	resp := e.do(t, http.MethodPost, "/api/admin/v1/users", map[string]any{
		"name": "Editor", "email": email, "password": "editor-strong-pass", "role": "editor",
	})
	if resp.StatusCode != http.StatusCreated {
		raw, _ := readJSON(t, resp)
		t.Fatalf("create editor status = %d; body = %s", resp.StatusCode, raw)
	}
	_ = resp.Body.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	resp = e.doWith(t, client, http.MethodPost, "/api/admin/v1/auth/login", map[string]any{
		"email": email, "password": "editor-strong-pass",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("editor login status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()
	return client
}

// revisionCount counts a post's revisions directly in the database.
func (e *testEnv) revisionCount(t *testing.T, postID string) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM post_revisions WHERE post_id = ?`, postID).Scan(&n); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	return n
}

// validationDetails extracts error.details as a field map, failing if
// absent.
func validationDetails(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	e, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("body has no error object: %v", m)
	}
	d, ok := e["details"].(map[string]any)
	if !ok {
		t.Fatalf("error has no details object: %v", m)
	}
	return d
}

func postField(t *testing.T, m map[string]any, key string) any {
	t.Helper()
	post, ok := m["post"].(map[string]any)
	if !ok {
		t.Fatalf("body has no post object: %v", m)
	}
	return post[key]
}

// ---------------------------------------------------------------------------
// Categories & tags
// ---------------------------------------------------------------------------

// TestCategoriesCRUDAndSlugRules covers auto slug generation, explicit slugs,
// slug conflicts on create and update, manual-slug preservation on update,
// and delete.
func TestCategoriesCRUDAndSlugRules(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	// Auto slug from name.
	resp := env.do(t, http.MethodPost, "/api/admin/v1/categories", map[string]any{"name": "Big News"})
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	if slug := m["category"].(map[string]any)["slug"]; slug != "big-news" {
		t.Fatalf("auto slug = %v, want big-news", slug)
	}

	// Explicit slug is normalized.
	resp = env.do(t, http.MethodPost, "/api/admin/v1/categories", map[string]any{"name": "Sports", "slug": "My Sports"})
	_, m = readJSON(t, resp)
	if slug := m["category"].(map[string]any)["slug"]; slug != "my-sports" {
		t.Fatalf("explicit slug = %v, want my-sports", slug)
	}

	// Slug conflict on create (never auto-suffixed).
	resp = env.do(t, http.MethodPost, "/api/admin/v1/categories", map[string]any{"name": "Dup", "slug": "big-news"})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "validation_failed" {
		t.Fatalf("slug conflict = %d %s, want 400 validation_failed", resp.StatusCode, errorCode(t, m))
	}
	if got := validationDetails(t, m)["slug"]; got != "This slug is already in use." {
		t.Fatalf("details.slug = %v, want the conflict message", got)
	}

	// A name that normalizes to nothing (and no slug) is a slug-empty error.
	resp = env.do(t, http.MethodPost, "/api/admin/v1/categories", map[string]any{"name": "!!!"})
	_, m = readJSON(t, resp)
	if got := validationDetails(t, m)["slug"]; got != "slug cannot be empty" {
		t.Fatalf("details.slug = %v, want 'slug cannot be empty'", got)
	}

	// Create one to update.
	tech := env.createCategory(t, "Tech")
	respTech := env.do(t, http.MethodPost, "/api/admin/v1/categories", map[string]any{"name": "Tech2", "slug": "tech-two"})
	_, _ = readJSON(t, respTech)

	// Update name only: slug is preserved.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/categories/"+tech, map[string]any{"name": "Technology"})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d, want 200", resp.StatusCode)
	}
	cat := m["category"].(map[string]any)
	if cat["name"] != "Technology" || cat["slug"] != "tech" {
		t.Fatalf("after name-only update = %v, want name Technology slug tech", cat)
	}

	// Update slug to a taken one → conflict.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/categories/"+tech, map[string]any{"slug": "tech-two"})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || validationDetails(t, m)["slug"] != "This slug is already in use." {
		t.Fatalf("update slug conflict = %d %v, want 400 conflict", resp.StatusCode, m)
	}

	// Delete → 204, and the slug becomes reusable.
	resp = env.do(t, http.MethodDelete, "/api/admin/v1/categories/"+tech, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = env.do(t, http.MethodPost, "/api/admin/v1/categories", map[string]any{"name": "Tech Again", "slug": "tech"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("reuse deleted slug status = %d, want 201", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Delete unknown → 404.
	resp = env.do(t, http.MethodDelete, "/api/admin/v1/categories/does-not-exist", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, m) != "not_found" {
		t.Fatalf("delete unknown = %d %s, want 404 not_found", resp.StatusCode, errorCode(t, m))
	}
}

// TestTagsCRUDAndSlugRules covers the tag equivalents: auto slug, conflict,
// list shape (no description) and delete.
func TestTagsCRUDAndSlugRules(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	id := env.createTag(t, "Go Lang")
	resp := env.do(t, http.MethodGet, "/api/admin/v1/tags", nil)
	raw, m := readJSON(t, resp)
	tags, _ := m["tags"].([]any)
	if len(tags) != 1 {
		t.Fatalf("tags length = %d, want 1", len(tags))
	}
	tag := tags[0].(map[string]any)
	if tag["slug"] != "go-lang" {
		t.Fatalf("tag slug = %v, want go-lang", tag["slug"])
	}
	if _, hasDesc := tag["description"]; hasDesc {
		t.Fatalf("tag DTO must not carry a description: %s", raw)
	}

	// Slug conflict.
	resp = env.do(t, http.MethodPost, "/api/admin/v1/tags", map[string]any{"name": "Dup", "slug": "go-lang"})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || validationDetails(t, m)["slug"] != "This slug is already in use." {
		t.Fatalf("tag slug conflict = %d %v, want 400 conflict", resp.StatusCode, m)
	}

	// Delete → 204.
	resp = env.do(t, http.MethodDelete, "/api/admin/v1/tags/"+id, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete tag status = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// ---------------------------------------------------------------------------
// Posts
// ---------------------------------------------------------------------------

// TestPostStatusGuard confirms Phase 3 rejects publish/schedule statuses and
// accepts draft/archived.
func TestPostStatusGuard(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	for _, status := range []string{"published", "scheduled"} {
		resp := env.do(t, http.MethodPost, "/api/admin/v1/posts", map[string]any{"title": "X " + status, "status": status})
		_, m := readJSON(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status %q = %d, want 400", status, resp.StatusCode)
		}
		if got := validationDetails(t, m)["status"]; got != "Publishing arrives in a later phase." {
			t.Fatalf("status %q details.status = %v, want the later-phase message", status, got)
		}
	}

	// Nonsense status.
	resp := env.do(t, http.MethodPost, "/api/admin/v1/posts", map[string]any{"title": "Bad", "status": "nonsense"})
	_, m := readJSON(t, resp)
	if got := validationDetails(t, m)["status"]; got != "Status must be 'draft' or 'archived'." {
		t.Fatalf("nonsense status details = %v", got)
	}

	// Draft default and archived allowed.
	draft := env.createPost(t, map[string]any{"title": "Draft Post"})
	if s := draft["status"]; s != "draft" {
		t.Fatalf("default status = %v, want draft", s)
	}
	archived := env.createPost(t, map[string]any{"title": "Archived Post", "status": "archived"})
	if s := archived["status"]; s != "archived" {
		t.Fatalf("archived status = %v, want archived", s)
	}
}

// TestPostSlugRules covers auto-slug on create, explicit slug, conflict, and
// preservation vs. re-normalization on update.
func TestPostSlugRules(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	first := env.createPost(t, map[string]any{"title": "My First Post"})
	if first["slug"] != "my-first-post" {
		t.Fatalf("auto slug = %v, want my-first-post", first["slug"])
	}

	second := env.createPost(t, map[string]any{"title": "Second", "slug": "custom-slug"})
	if second["slug"] != "custom-slug" {
		t.Fatalf("explicit slug = %v, want custom-slug", second["slug"])
	}

	// Slug conflict on create.
	resp := env.do(t, http.MethodPost, "/api/admin/v1/posts", map[string]any{"title": "Third", "slug": "my-first-post"})
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || validationDetails(t, m)["slug"] != "This slug is already in use." {
		t.Fatalf("create slug conflict = %d %v, want 400 conflict", resp.StatusCode, m)
	}

	// Manual slug preserved when slug omitted on update.
	secondID := second["id"].(string)
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/posts/"+secondID, map[string]any{"title": "Second Renamed"})
	_, m = readJSON(t, resp)
	if postField(t, m, "slug") != "custom-slug" || postField(t, m, "title") != "Second Renamed" {
		t.Fatalf("after title-only update = slug %v title %v, want custom-slug/Second Renamed",
			postField(t, m, "slug"), postField(t, m, "title"))
	}

	// Provided slug is normalized on update.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/posts/"+secondID, map[string]any{"slug": "My New Slug"})
	_, m = readJSON(t, resp)
	if postField(t, m, "slug") != "my-new-slug" {
		t.Fatalf("updated slug = %v, want my-new-slug", postField(t, m, "slug"))
	}

	// Updating another post to that slug conflicts.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/posts/"+first["id"].(string), map[string]any{"slug": "my-new-slug"})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || validationDetails(t, m)["slug"] != "This slug is already in use." {
		t.Fatalf("update slug conflict = %d %v, want 400 conflict", resp.StatusCode, m)
	}
}

// TestPostContentPipeline confirms the create pipeline: content_json is stored
// verbatim, content_html is sanitized server-side (attack vectors removed,
// legitimate markup preserved), and the internal content_text is not exposed.
func TestPostContentPipeline(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	// A realistic serialized editor state (opaque to the server) plus a
	// client HTML export laced with injection attempts.
	contentJSON := `{"root":{"type":"root","children":[{"type":"heading","tag":"h1","children":[{"type":"text","text":"Title"}]},{"type":"paragraph","children":[{"type":"text","format":1,"text":"Hello world"}]}]}}`
	contentHTML := `<h1>Title</h1><p>Hello <strong>world</strong></p>` +
		`<script>alert('xss')</script>` +
		`<p>Link <a href="javascript:alert(1)">bad</a> and <a href="https://ok.com">good</a></p>` +
		`<img src="/api/files/a.png" alt="pic"><img src="x" onerror="alert(1)">` +
		`<img src="data:image/png;base64,AAAA" alt="d">`

	post := env.createPost(t, map[string]any{
		"title":        "Pipeline",
		"content_json": contentJSON,
		"content_html": contentHTML,
	})

	// content_json round-trips byte-for-byte (opaque, unmodified).
	if post["content_json"] != contentJSON {
		t.Fatalf("content_json round-trip = %v, want verbatim input", post["content_json"])
	}

	// content_text is internal and must never appear in a response.
	if _, ok := post["content_text"]; ok {
		t.Fatalf("response leaked internal content_text: %v", post["content_text"])
	}

	// content_html is the sanitized export: attack vectors gone, legit intact.
	got, _ := post["content_html"].(string)
	for _, bad := range []string{"script", "alert", "javascript:", "onerror", "data:", "base64"} {
		if strings.Contains(strings.ToLower(got), bad) {
			t.Fatalf("sanitized content_html still contains %q: %s", bad, got)
		}
	}
	for _, good := range []string{
		"<h1>Title</h1>",
		"<strong>world</strong>",
		`<a href="https://ok.com">good</a>`,
		`<img src="/api/files/a.png" alt="pic">`,
		`<img src="x">`, // onerror stripped, relative src kept
	} {
		if !strings.Contains(got, good) {
			t.Fatalf("sanitized content_html missing legit markup %q: %s", good, got)
		}
	}
	// The javascript: link text survives even though its href was dropped.
	if !strings.Contains(got, "bad") {
		t.Fatalf("javascript link text was lost: %s", got)
	}
}

// TestPostContentValidation covers the content_json / content_html validation
// contract: empty is valid, invalid JSON and oversize payloads are field
// errors.
func TestPostContentValidation(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	// Both empty → valid title-only draft; content_json stored as "{}".
	post := env.createPost(t, map[string]any{"title": "Title Only"})
	if post["content_json"] != "{}" {
		t.Fatalf("empty content_json stored as %v, want {}", post["content_json"])
	}
	if post["content_html"] != "" {
		t.Fatalf("empty content_html stored as %v, want empty", post["content_html"])
	}

	// Invalid JSON in content_json → 400 with details.content_json.
	resp := env.do(t, http.MethodPost, "/api/admin/v1/posts", map[string]any{
		"title": "Bad JSON", "content_json": "{not valid json",
	})
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "validation_failed" {
		t.Fatalf("invalid content_json = %d %s, want 400 validation_failed", resp.StatusCode, errorCode(t, m))
	}
	if _, ok := validationDetails(t, m)["content_json"]; !ok {
		t.Fatalf("invalid content_json: details missing content_json: %v", m)
	}

	// Oversize content_json (> 2 MB) → 400 with details.content_json.
	oversize := strings.Repeat("a", (2<<20)+1)
	resp = env.do(t, http.MethodPost, "/api/admin/v1/posts", map[string]any{
		"title": "Big JSON", "content_json": oversize,
	})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversize content_json = %d, want 400", resp.StatusCode)
	}
	if _, ok := validationDetails(t, m)["content_json"]; !ok {
		t.Fatalf("oversize content_json: details missing content_json: %v", m)
	}

	// Oversize content_html (> 2 MB) → 400 with details.content_html.
	resp = env.do(t, http.MethodPost, "/api/admin/v1/posts", map[string]any{
		"title": "Big HTML", "content_html": oversize,
	})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversize content_html = %d, want 400", resp.StatusCode)
	}
	if _, ok := validationDetails(t, m)["content_html"]; !ok {
		t.Fatalf("oversize content_html: details missing content_html: %v", m)
	}
}

// TestPostRevisions confirms create writes revision #1, a changed save adds a
// revision, and a no-op or metadata-only save does not. Revision dedup now
// compares title, excerpt, content_json and content_html.
func TestPostRevisions(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	post := env.createPost(t, map[string]any{
		"title": "Rev", "content_html": "<p>hello</p>", "content_json": `{"v":1}`,
	})
	id := post["id"].(string)
	if n := env.revisionCount(t, id); n != 1 {
		t.Fatalf("revisions after create = %d, want 1", n)
	}

	// No-op save (same title, content unchanged) → no revision.
	resp := env.do(t, http.MethodPatch, "/api/admin/v1/posts/"+id, map[string]any{"title": "Rev"})
	_ = resp.Body.Close()
	if n := env.revisionCount(t, id); n != 1 {
		t.Fatalf("revisions after no-op save = %d, want 1", n)
	}

	// Metadata-only change (featured) → no revision.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/posts/"+id, map[string]any{"featured": true})
	_ = resp.Body.Close()
	if n := env.revisionCount(t, id); n != 1 {
		t.Fatalf("revisions after metadata-only save = %d, want 1", n)
	}

	// Title change → new revision.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/posts/"+id, map[string]any{"title": "Rev Edited"})
	_ = resp.Body.Close()
	if n := env.revisionCount(t, id); n != 2 {
		t.Fatalf("revisions after title change = %d, want 2", n)
	}

	// content_html change → new revision.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/posts/"+id, map[string]any{"content_html": "<p>world</p>"})
	_ = resp.Body.Close()
	if n := env.revisionCount(t, id); n != 3 {
		t.Fatalf("revisions after content_html change = %d, want 3", n)
	}

	// content_json change (HTML unchanged) → new revision.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/posts/"+id, map[string]any{"content_json": `{"v":2}`})
	_ = resp.Body.Close()
	if n := env.revisionCount(t, id); n != 4 {
		t.Fatalf("revisions after content_json change = %d, want 4", n)
	}
}

// TestPostDuplicate covers the duplicate contract: copied fields, forced
// draft, and "-copy" slug sequencing.
func TestPostDuplicate(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	catID := env.createCategory(t, "News")
	tagID := env.createTag(t, "Go")
	orig := env.createPost(t, map[string]any{
		"title": "Original", "slug": "original",
		"content_html": "<p>body</p>", "content_json": `{"k":"v"}`,
		"status": "archived", "featured": true,
		"categories": []string{catID}, "tags": []string{tagID},
	})
	origID := orig["id"].(string)

	// First duplicate.
	resp := env.do(t, http.MethodPost, "/api/admin/v1/posts/"+origID+"/duplicate", nil)
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("duplicate status = %d, want 201; body = %s", resp.StatusCode, raw)
	}
	dup := m["post"].(map[string]any)
	if dup["title"] != "Copy of Original" {
		t.Fatalf("duplicate title = %v, want 'Copy of Original'", dup["title"])
	}
	if dup["slug"] != "original-copy" {
		t.Fatalf("duplicate slug = %v, want original-copy", dup["slug"])
	}
	if dup["status"] != "draft" {
		t.Fatalf("duplicate status = %v, want draft (forced)", dup["status"])
	}
	if dup["featured"] != true {
		t.Fatalf("duplicate featured = %v, want true (copied)", dup["featured"])
	}
	if dup["content_html"] != "<p>body</p>" {
		t.Fatalf("duplicate content_html = %v, want the copied body", dup["content_html"])
	}
	if dup["content_json"] != `{"k":"v"}` {
		t.Fatalf("duplicate content_json = %v, want the copied json", dup["content_json"])
	}
	if cats, _ := dup["categories"].([]any); len(cats) != 1 {
		t.Fatalf("duplicate categories = %v, want the copied one", dup["categories"])
	}
	if tags, _ := dup["tags"].([]any); len(tags) != 1 {
		t.Fatalf("duplicate tags = %v, want the copied one", dup["tags"])
	}
	if dup["id"] == origID {
		t.Fatal("duplicate reused the original id")
	}

	// Second duplicate of the original → -copy-2.
	resp = env.do(t, http.MethodPost, "/api/admin/v1/posts/"+origID+"/duplicate", nil)
	_, m = readJSON(t, resp)
	if postField(t, m, "slug") != "original-copy-2" {
		t.Fatalf("second duplicate slug = %v, want original-copy-2", postField(t, m, "slug"))
	}
}

// TestPostListFiltersAndPagination covers status/category/search filtering,
// pagination math and the sort directions.
func TestPostListFiltersAndPagination(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	catID := env.createCategory(t, "News")
	a := env.createPost(t, map[string]any{"title": "Alpha", "categories": []string{catID}})
	_ = env.createPost(t, map[string]any{"title": "Beta", "status": "archived"})
	c := env.createPost(t, map[string]any{"title": "Gamma"})

	// All posts, default sort (-updated_at → newest first: Gamma, Beta, Alpha).
	resp := env.do(t, http.MethodGet, "/api/admin/v1/posts", nil)
	_, m := readJSON(t, resp)
	meta := m["meta"].(map[string]any)
	if meta["total_items"].(float64) != 3 || meta["total_pages"].(float64) != 1 || meta["page"].(float64) != 1 {
		t.Fatalf("meta = %v, want total_items 3, total_pages 1, page 1", meta)
	}
	posts := m["posts"].([]any)
	if len(posts) != 3 || posts[0].(map[string]any)["title"] != "Gamma" {
		t.Fatalf("default sort first = %v, want Gamma newest-first", posts[0])
	}
	_ = a
	_ = c

	// Ascending sort → oldest first (Alpha).
	resp = env.do(t, http.MethodGet, "/api/admin/v1/posts?sort=updated_at", nil)
	_, m = readJSON(t, resp)
	if m["posts"].([]any)[0].(map[string]any)["title"] != "Alpha" {
		t.Fatalf("ascending sort first = %v, want Alpha", m["posts"].([]any)[0])
	}

	// Status filter.
	resp = env.do(t, http.MethodGet, "/api/admin/v1/posts?status=archived", nil)
	_, m = readJSON(t, resp)
	if m["meta"].(map[string]any)["total_items"].(float64) != 1 {
		t.Fatalf("status=archived total = %v, want 1", m["meta"].(map[string]any)["total_items"])
	}

	// Category filter.
	resp = env.do(t, http.MethodGet, "/api/admin/v1/posts?category="+catID, nil)
	_, m = readJSON(t, resp)
	if m["meta"].(map[string]any)["total_items"].(float64) != 1 {
		t.Fatalf("category filter total = %v, want 1", m["meta"].(map[string]any)["total_items"])
	}

	// Search (FTS prefix over title/excerpt/body).
	resp = env.do(t, http.MethodGet, "/api/admin/v1/posts?search=ALPH", nil)
	_, m = readJSON(t, resp)
	if m["meta"].(map[string]any)["total_items"].(float64) != 1 {
		t.Fatalf("search=ALPH total = %v, want 1", m["meta"].(map[string]any)["total_items"])
	}

	// Pagination: per_page 2 over 3 items.
	resp = env.do(t, http.MethodGet, "/api/admin/v1/posts?per_page=2", nil)
	_, m = readJSON(t, resp)
	meta = m["meta"].(map[string]any)
	if meta["per_page"].(float64) != 2 || meta["total_pages"].(float64) != 2 {
		t.Fatalf("per_page=2 meta = %v, want per_page 2 total_pages 2", meta)
	}
	if len(m["posts"].([]any)) != 2 {
		t.Fatalf("per_page=2 page 1 len = %d, want 2", len(m["posts"].([]any)))
	}
	resp = env.do(t, http.MethodGet, "/api/admin/v1/posts?per_page=2&page=2", nil)
	_, m = readJSON(t, resp)
	if len(m["posts"].([]any)) != 1 {
		t.Fatalf("per_page=2 page 2 len = %d, want 1", len(m["posts"].([]any)))
	}

	// per_page above the max is clamped to 100.
	resp = env.do(t, http.MethodGet, "/api/admin/v1/posts?per_page=500", nil)
	_, m = readJSON(t, resp)
	if m["meta"].(map[string]any)["per_page"].(float64) != 100 {
		t.Fatalf("per_page=500 clamped = %v, want 100", m["meta"].(map[string]any)["per_page"])
	}
}

// TestPostUnknownReferences confirms unknown author/category/tag ids are
// field-level validation errors.
func TestPostUnknownReferences(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	cases := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"author", map[string]any{"title": "A", "author": "nope"}, "author"},
		{"categories", map[string]any{"title": "B", "categories": []string{"nope"}}, "categories"},
		{"tags", map[string]any{"title": "C", "tags": []string{"nope"}}, "tags"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := env.do(t, http.MethodPost, "/api/admin/v1/posts", tc.body)
			_, m := readJSON(t, resp)
			if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "validation_failed" {
				t.Fatalf("%s = %d %s, want 400 validation_failed", tc.name, resp.StatusCode, errorCode(t, m))
			}
			if _, ok := validationDetails(t, m)[tc.field]; !ok {
				t.Fatalf("%s: details missing field %q: %v", tc.name, tc.field, m)
			}
		})
	}
}

// TestPostGetAndDelete covers the single-post fetch and delete 404 behavior.
func TestPostGetAndDelete(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	post := env.createPost(t, map[string]any{"title": "Fetch me"})
	id := post["id"].(string)

	resp := env.do(t, http.MethodGet, "/api/admin/v1/posts/"+id, nil)
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK || postField(t, m, "title") != "Fetch me" {
		t.Fatalf("get post = %d %v, want 200 Fetch me", resp.StatusCode, m)
	}

	resp = env.do(t, http.MethodDelete, "/api/admin/v1/posts/"+id, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = env.do(t, http.MethodGet, "/api/admin/v1/posts/"+id, nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, m) != "not_found" {
		t.Fatalf("get after delete = %d %s, want 404 not_found", resp.StatusCode, errorCode(t, m))
	}
}

// ---------------------------------------------------------------------------
// Authors, permissions
// ---------------------------------------------------------------------------

// TestAuthorsEndpointHidesEmail confirms /authors returns only id/name for
// active users and never leaks email or role.
func TestAuthorsEndpointHidesEmail(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	// Create an editor, then list authors: owner + editor.
	resp := env.do(t, http.MethodPost, "/api/admin/v1/users", map[string]any{
		"name": "Ed Writer", "email": "editor@example.com", "password": "editor-strong-pass", "role": "editor",
	})
	_, m := readJSON(t, resp)
	editorID := m["user"].(map[string]any)["id"].(string)

	resp = env.do(t, http.MethodGet, "/api/admin/v1/authors", nil)
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authors status = %d, want 200", resp.StatusCode)
	}
	authors := m["authors"].([]any)
	if len(authors) != 2 {
		t.Fatalf("authors length = %d, want 2", len(authors))
	}
	for _, a := range authors {
		am := a.(map[string]any)
		if am["id"] == nil || am["name"] == nil {
			t.Fatalf("author missing id/name: %v", am)
		}
		if _, ok := am["email"]; ok {
			t.Fatalf("author leaked email: %v", am)
		}
		if _, ok := am["role"]; ok {
			t.Fatalf("author leaked role: %v", am)
		}
	}
	if strings.Contains(string(raw), "editor@example.com") || strings.Contains(string(raw), "owner@example.com") {
		t.Fatalf("authors response leaked an email address: %s", raw)
	}

	// Deactivating the editor removes it from the author list.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/users/"+editorID, map[string]any{"active": false})
	_ = resp.Body.Close()
	resp = env.do(t, http.MethodGet, "/api/admin/v1/authors", nil)
	_, m = readJSON(t, resp)
	if len(m["authors"].([]any)) != 1 {
		t.Fatalf("authors after deactivation = %d, want 1 (active only)", len(m["authors"].([]any)))
	}
}

// TestEditorCanCRUDContent confirms an editor (not owner) can manage
// categories, tags and posts.
func TestEditorCanCRUDContent(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	editor := env.newEditorClient(t, "editor@example.com")

	// Category.
	resp := env.doWith(t, editor, http.MethodPost, "/api/admin/v1/categories", map[string]any{"name": "Editor Cat"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("editor create category = %d, want 201", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Tag.
	resp = env.doWith(t, editor, http.MethodPost, "/api/admin/v1/tags", map[string]any{"name": "Editor Tag"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("editor create tag = %d, want 201", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Post: create, patch, delete.
	resp = env.doWith(t, editor, http.MethodPost, "/api/admin/v1/posts", map[string]any{"title": "Editor Post"})
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("editor create post = %d, want 201; body = %s", resp.StatusCode, raw)
	}
	postID := m["post"].(map[string]any)["id"].(string)
	// The post is authored by the editor (default author = current user).
	if author := m["post"].(map[string]any)["author"].(map[string]any); author["name"] != "Editor" {
		t.Fatalf("editor post author = %v, want the editor", author)
	}

	resp = env.doWith(t, editor, http.MethodPatch, "/api/admin/v1/posts/"+postID, map[string]any{"title": "Edited by Editor"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("editor patch post = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = env.doWith(t, editor, http.MethodDelete, "/api/admin/v1/posts/"+postID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("editor delete post = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestContentUnauthenticated confirms every content endpoint 401s without a
// session.
func TestContentUnauthenticated(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	anon := &http.Client{}

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/admin/v1/authors"},
		{http.MethodGet, "/api/admin/v1/categories"},
		{http.MethodPost, "/api/admin/v1/categories"},
		{http.MethodGet, "/api/admin/v1/tags"},
		{http.MethodPost, "/api/admin/v1/tags"},
		{http.MethodGet, "/api/admin/v1/posts"},
		{http.MethodPost, "/api/admin/v1/posts"},
	} {
		resp := env.doWith(t, anon, tc.method, tc.path, nil)
		_, m := readJSON(t, resp)
		if resp.StatusCode != http.StatusUnauthorized || errorCode(t, m) != "unauthorized" {
			t.Fatalf("%s %s anon = %d %s, want 401 unauthorized", tc.method, tc.path, resp.StatusCode, errorCode(t, m))
		}
	}
}
