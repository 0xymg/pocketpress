package http

import (
	"net/http"
	"testing"
	"time"
)

// TestPublicAuthorsEndpoints covers list/get/posts for richer author profiles.
func TestPublicAuthorsEndpoints(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	ownerID := env.do(t, http.MethodGet, "/api/admin/v1/auth/me", nil)
	_, me := readJSON(t, ownerID)
	owner := me["user"].(map[string]any)
	ownerSlug, _ := owner["slug"].(string)
	if ownerSlug == "" {
		t.Fatal("owner slug empty after setup")
	}

	// Enrich the owner's public profile via PATCH /auth/me.
	resp := env.do(t, http.MethodPatch, "/api/admin/v1/auth/me", map[string]any{
		"bio":      "Writes about tiny blogs.",
		"website":  "https://example.com",
		"location": "Berlin",
		"slug":     "yunus",
	})
	_, patched := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /auth/me = %d %v", resp.StatusCode, patched)
	}
	ownerSlug = patched["user"].(map[string]any)["slug"].(string)
	if ownerSlug != "yunus" {
		t.Fatalf("slug after patch = %q, want yunus", ownerSlug)
	}

	// Create and publish a post.
	resp = env.do(t, http.MethodPost, "/api/admin/v1/posts", map[string]any{
		"title": "Hello Authors",
		"slug":  "hello-authors",
	})
	_, postBody := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create post = %d %v", resp.StatusCode, postBody)
	}
	postID := postBody["post"].(map[string]any)["id"].(string)
	now := time.Now().UTC().Add(-time.Minute)
	env.setPostState(t, postID, "published", &now)

	// List authors.
	resp = env.do(t, http.MethodGet, "/api/v1/authors", nil)
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /authors = %d %s", resp.StatusCode, raw)
	}
	data := m["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("authors len = %d, want 1", len(data))
	}
	a := data[0].(map[string]any)
	if a["slug"] != "yunus" || a["name"] == nil || a["bio"] != "Writes about tiny blogs." {
		t.Fatalf("author list item = %#v", a)
	}
	if a["website"] != "https://example.com" || a["location"] != "Berlin" {
		t.Fatalf("author profile fields = %#v", a)
	}
	if _, ok := a["email"]; ok {
		t.Fatalf("author leaked email: %s", raw)
	}

	// Get by slug.
	resp = env.do(t, http.MethodGet, "/api/v1/authors/yunus", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /authors/yunus = %d", resp.StatusCode)
	}
	if m["data"].(map[string]any)["slug"] != "yunus" {
		t.Fatalf("get author = %#v", m["data"])
	}

	resp = env.do(t, http.MethodGet, "/api/v1/authors/missing", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, m) != "author_not_found" {
		t.Fatalf("missing author = %d %s", resp.StatusCode, errorCode(t, m))
	}

	// Author posts by slug.
	resp = env.do(t, http.MethodGet, "/api/v1/authors/yunus/posts", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("author posts = %d", resp.StatusCode)
	}
	posts := m["data"].([]any)
	if len(posts) != 1 {
		t.Fatalf("author posts len = %d, want 1", len(posts))
	}
	embedded := posts[0].(map[string]any)["author"].(map[string]any)
	if embedded["slug"] != "yunus" || embedded["bio"] != "Writes about tiny blogs." {
		t.Fatalf("embedded author = %#v", embedded)
	}

	// Filter posts by author slug.
	resp = env.do(t, http.MethodGet, "/api/v1/posts?author=yunus", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK || len(m["data"].([]any)) != 1 {
		t.Fatalf("posts?author=yunus = %d %#v", resp.StatusCode, m["data"])
	}

	// Create a second author and confirm slug collision on profile update.
	resp = env.do(t, http.MethodPost, "/api/admin/v1/users", map[string]any{
		"name":     "Grace Hopper",
		"email":    "grace@example.com",
		"password": "grace-strong-pass",
		"role":     "editor",
	})
	_, created := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create editor = %d %v", resp.StatusCode, created)
	}
	editorID := created["user"].(map[string]any)["id"].(string)
	editorSlug := created["user"].(map[string]any)["slug"].(string)
	if editorSlug == "" || editorSlug == "yunus" {
		t.Fatalf("editor slug = %q", editorSlug)
	}

	resp = env.do(t, http.MethodPatch, "/api/admin/v1/users/"+editorID, map[string]any{
		"slug": "yunus",
	})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "validation_failed" {
		t.Fatalf("slug collision = %d %s", resp.StatusCode, errorCode(t, m))
	}
}
