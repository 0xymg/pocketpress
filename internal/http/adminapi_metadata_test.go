package http

import (
	"net/http"
	"testing"
	"time"
)

// TestPostCustomMetadataRoundTrip covers create/update/duplicate/public
// exposure of the per-post custom metadata object (v0.3).
func TestPostCustomMetadataRoundTrip(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	created := env.createPost(t, map[string]any{
		"title":    "Meta Post",
		"slug":     "meta-post",
		"metadata": map[string]any{"hero": "launch", "priority": float64(1)},
	})
	meta, ok := created["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("create metadata = %T %v, want object", created["metadata"], created["metadata"])
	}
	if meta["hero"] != "launch" {
		t.Fatalf("create metadata.hero = %v, want launch", meta["hero"])
	}

	resp := env.do(t, http.MethodPatch, "/api/admin/v1/posts/"+created["id"].(string), map[string]any{
		"metadata": map[string]any{"hero": "shipped", "flags": []any{"a", "b"}},
	})
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d %v", resp.StatusCode, m)
	}
	post := m["post"].(map[string]any)
	meta = post["metadata"].(map[string]any)
	if meta["hero"] != "shipped" {
		t.Fatalf("patch metadata.hero = %v, want shipped", meta["hero"])
	}

	// Non-object rejected.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/posts/"+created["id"].(string), map[string]any{
		"metadata": []any{"nope"},
	})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("array metadata status = %d, want 400", resp.StatusCode)
	}
	if _, ok := validationDetails(t, m)["metadata"]; !ok {
		t.Fatalf("array metadata: details missing metadata: %v", m)
	}

	// Duplicate copies metadata.
	resp = env.do(t, http.MethodPost, "/api/admin/v1/posts/"+created["id"].(string)+"/duplicate", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("duplicate status = %d %v", resp.StatusCode, m)
	}
	dup := m["post"].(map[string]any)
	dupMeta := dup["metadata"].(map[string]any)
	if dupMeta["hero"] != "shipped" {
		t.Fatalf("duplicate metadata.hero = %v, want shipped", dupMeta["hero"])
	}

	// Public API exposes metadata on published posts.
	env.publishPost(t, created["id"].(string), time.Now().UTC().Add(-time.Minute))
	resp = env.do(t, http.MethodGet, "/api/v1/posts/meta-post", nil)
	_, m = readJSON(t, resp)
	data := dataObj(t, m)
	pubMeta, ok := data["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("public metadata = %T %v", data["metadata"], data["metadata"])
	}
	if pubMeta["hero"] != "shipped" {
		t.Fatalf("public metadata.hero = %v, want shipped", pubMeta["hero"])
	}

	// Bare create defaults to {}.
	bare := env.createPost(t, map[string]any{"title": "Bare Meta", "slug": "bare-meta"})
	bareMeta, ok := bare["metadata"].(map[string]any)
	if !ok || len(bareMeta) != 0 {
		t.Fatalf("bare metadata = %v, want {}", bare["metadata"])
	}
}
