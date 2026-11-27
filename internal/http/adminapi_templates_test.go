package http

import (
	"net/http"
	"testing"
)

// TestTemplatesCRUD covers create (from fields + from_post), list, get,
// update, delete, and create-post from template.
func TestTemplatesCRUD(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	catID := env.createCategory(t, "News")
	tagID := env.createTag(t, "Release")

	// Create from fields.
	resp := env.do(t, http.MethodPost, "/api/admin/v1/templates", map[string]any{
		"name":         "Changelog",
		"description":  "Monthly changelog starter",
		"title":        "Changelog entry",
		"excerpt":      "What shipped this month.",
		"content_html": "<p>Highlights</p>",
		"categories":   []string{catID},
		"tags":         []string{tagID},
		"featured":     true,
		"seo_title":    "Changelog SEO",
	})
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create template status = %d; body = %s", resp.StatusCode, raw)
	}
	tmpl, _ := m["template"].(map[string]any)
	if tmpl["name"] != "Changelog" {
		t.Fatalf("name = %v, want Changelog", tmpl["name"])
	}
	if tmpl["title"] != "Changelog entry" {
		t.Fatalf("title = %v", tmpl["title"])
	}
	if tmpl["featured"] != true {
		t.Fatalf("featured = %v, want true", tmpl["featured"])
	}
	cats, _ := tmpl["categories"].([]any)
	if len(cats) != 1 {
		t.Fatalf("categories = %v, want 1", cats)
	}
	tmplID, _ := tmpl["id"].(string)
	if tmplID == "" {
		t.Fatalf("missing template id: %s", raw)
	}

	// List.
	resp = env.do(t, http.MethodGet, "/api/admin/v1/templates", nil)
	raw, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list templates status = %d; body = %s", resp.StatusCode, raw)
	}
	list, _ := m["templates"].([]any)
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1; body = %s", len(list), raw)
	}

	// Get.
	resp = env.do(t, http.MethodGet, "/api/admin/v1/templates/"+tmplID, nil)
	raw, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get template status = %d; body = %s", resp.StatusCode, raw)
	}
	if m["template"].(map[string]any)["id"] != tmplID {
		t.Fatalf("get id mismatch: %s", raw)
	}

	// Update name.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/templates/"+tmplID, map[string]any{
		"name": "Release notes",
	})
	raw, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch template status = %d; body = %s", resp.StatusCode, raw)
	}
	if m["template"].(map[string]any)["name"] != "Release notes" {
		t.Fatalf("patched name = %v", m["template"].(map[string]any)["name"])
	}

	// Create post from template.
	resp = env.do(t, http.MethodPost, "/api/admin/v1/templates/"+tmplID+"/create-post", nil)
	raw, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create-post status = %d; body = %s", resp.StatusCode, raw)
	}
	post, _ := m["post"].(map[string]any)
	if post["title"] != "Changelog entry" {
		t.Fatalf("post title = %v, want template title", post["title"])
	}
	if post["status"] != "draft" {
		t.Fatalf("post status = %v, want draft", post["status"])
	}
	if post["seo_title"] != "Changelog SEO" {
		t.Fatalf("post seo_title = %v", post["seo_title"])
	}
	postCats, _ := post["categories"].([]any)
	if len(postCats) != 1 {
		t.Fatalf("post categories = %v, want 1", postCats)
	}
	postTags, _ := post["tags"].([]any)
	if len(postTags) != 1 {
		t.Fatalf("post tags = %v, want 1", postTags)
	}

	// Delete.
	resp = env.do(t, http.MethodDelete, "/api/admin/v1/templates/"+tmplID, nil)
	if resp.StatusCode != http.StatusNoContent {
		raw, _ = readJSON(t, resp)
		t.Fatalf("delete status = %d; body = %s", resp.StatusCode, raw)
	}
	_ = resp.Body.Close()

	resp = env.do(t, http.MethodGet, "/api/admin/v1/templates/"+tmplID, nil)
	raw, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete = %d; body = %s", resp.StatusCode, raw)
	}
}

// TestTemplateFromPost copies fields from an existing post into a template.
func TestTemplateFromPost(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	catID := env.createCategory(t, "Guides")
	post := env.createPost(t, map[string]any{
		"title":        "How to ship",
		"excerpt":      "A shipping guide",
		"content_html": "<p>Step one</p>",
		"categories":   []string{catID},
		"featured":     true,
		"seo_title":    "Ship guide",
	})
	postID, _ := post["id"].(string)

	resp := env.do(t, http.MethodPost, "/api/admin/v1/templates", map[string]any{
		"name":      "Shipping guide template",
		"from_post": postID,
	})
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("from_post create status = %d; body = %s", resp.StatusCode, raw)
	}
	tmpl := m["template"].(map[string]any)
	if tmpl["title"] != "How to ship" {
		t.Fatalf("copied title = %v", tmpl["title"])
	}
	if tmpl["excerpt"] != "A shipping guide" {
		t.Fatalf("copied excerpt = %v", tmpl["excerpt"])
	}
	if tmpl["seo_title"] != "Ship guide" {
		t.Fatalf("copied seo_title = %v", tmpl["seo_title"])
	}
	if tmpl["featured"] != true {
		t.Fatalf("copied featured = %v", tmpl["featured"])
	}
	cats, _ := tmpl["categories"].([]any)
	if len(cats) != 1 {
		t.Fatalf("copied categories = %v", cats)
	}
}

// TestTemplateValidation covers missing name and unknown from_post.
func TestTemplateValidation(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	resp := env.do(t, http.MethodPost, "/api/admin/v1/templates", map[string]any{
		"title": "No name",
	})
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing name status = %d; body = %s", resp.StatusCode, raw)
	}
	details := validationDetails(t, m)
	if details["name"] == nil {
		t.Fatalf("expected name field error: %s", raw)
	}

	resp = env.do(t, http.MethodPost, "/api/admin/v1/templates", map[string]any{
		"name":      "Broken",
		"from_post": "00000000-0000-7000-8000-000000000000",
	})
	raw, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad from_post status = %d; body = %s", resp.StatusCode, raw)
	}
	details = validationDetails(t, m)
	if details["from_post"] == nil {
		t.Fatalf("expected from_post field error: %s", raw)
	}
}

// TestTemplateCreatePostAuthRequiresSession confirms unauthenticated access
// is rejected.
func TestTemplateCreatePostAuthRequiresSession(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	resp := env.do(t, http.MethodPost, "/api/admin/v1/templates", map[string]any{
		"name":  "Auth check",
		"title": "T",
	})
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup template = %d; body = %s", resp.StatusCode, raw)
	}
	id := m["template"].(map[string]any)["id"].(string)

	// Bare client without session cookie.
	bare := &http.Client{}
	resp = env.doWith(t, bare, http.MethodGet, "/api/admin/v1/templates", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth list = %d, want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = env.doWith(t, bare, http.MethodPost, "/api/admin/v1/templates/"+id+"/create-post", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth create-post = %d, want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestTemplateCreatePostUntitledFallback uses "Untitled" when template title
// is blank.
func TestTemplateCreatePostUntitledFallback(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	resp := env.do(t, http.MethodPost, "/api/admin/v1/templates", map[string]any{
		"name": "Blank title",
	})
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d; body = %s", resp.StatusCode, raw)
	}
	id := m["template"].(map[string]any)["id"].(string)

	resp = env.do(t, http.MethodPost, "/api/admin/v1/templates/"+id+"/create-post", nil)
	raw, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create-post = %d; body = %s", resp.StatusCode, raw)
	}
	if m["post"].(map[string]any)["title"] != "Untitled" {
		t.Fatalf("title = %v, want Untitled", m["post"].(map[string]any)["title"])
	}
}
