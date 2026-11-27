package http

import (
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestPublicSitemap covers GET /api/v1/sitemap.xml: Content-Type,
// Cache-Control, homepage + published posts, draft exclusion, and
// canonical_url vs site_url/slug permalinks.
func TestPublicSitemap(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	now := time.Now().UTC()

	older := env.createPost(t, map[string]any{
		"title": "Older Post", "slug": "older-post",
		"excerpt": "Older excerpt.",
	})
	env.publishPost(t, older["id"].(string), now.Add(-2*time.Hour))

	newer := env.createPost(t, map[string]any{
		"title": "Newer Post", "slug": "newer-post",
		"excerpt":       "Newer excerpt.",
		"canonical_url": "https://example.com/blog/newer-post",
	})
	env.publishPost(t, newer["id"].(string), now.Add(-time.Hour))

	draft := env.createPost(t, map[string]any{
		"title": "Secret Draft", "slug": "secret-draft",
		"excerpt": "Should not appear.",
	})
	_ = draft

	resp := env.do(t, http.MethodGet, "/api/v1/sitemap.xml", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /sitemap.xml status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/xml; charset=utf-8" {
		t.Fatalf("GET /sitemap.xml Content-Type = %q, want application/xml; charset=utf-8", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=60" {
		t.Fatalf("GET /sitemap.xml Cache-Control = %q, want public, max-age=60", cc)
	}
	if nosniff := resp.Header.Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("GET /sitemap.xml X-Content-Type-Options = %q, want nosniff", nosniff)
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read sitemap body: %v", err)
	}
	raw := string(body)
	if !strings.HasPrefix(raw, xml.Header) && !strings.HasPrefix(raw, "<?xml") {
		t.Fatalf("sitemap missing XML declaration: %q", raw[:min(80, len(raw))])
	}

	var doc struct {
		XMLNS string `xml:"xmlns,attr"`
		URLs  []struct {
			Loc     string `xml:"loc"`
			LastMod string `xml:"lastmod"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("unmarshal sitemap: %v\nbody=%s", err, raw)
	}
	if doc.XMLNS != "http://www.sitemaps.org/schemas/sitemap/0.9" {
		t.Fatalf("sitemap xmlns = %q, want sitemap.org 0.9", doc.XMLNS)
	}
	// Homepage + 2 published posts; draft excluded.
	if len(doc.URLs) != 3 {
		t.Fatalf("sitemap urls = %d, want 3 (homepage + 2 posts); body=%s", len(doc.URLs), raw)
	}
	if doc.URLs[0].Loc != "https://example.com" {
		t.Fatalf("sitemap first loc = %q, want site homepage", doc.URLs[0].Loc)
	}
	if doc.URLs[1].Loc != "https://example.com/blog/newer-post" {
		t.Fatalf("sitemap newer loc = %q, want canonical_url", doc.URLs[1].Loc)
	}
	if doc.URLs[2].Loc != "https://example.com/older-post" {
		t.Fatalf("sitemap older loc = %q, want site_url/slug fallback", doc.URLs[2].Loc)
	}
	if doc.URLs[1].LastMod == "" || doc.URLs[2].LastMod == "" {
		t.Fatalf("sitemap post entries missing lastmod: %+v", doc.URLs)
	}
	if strings.Contains(raw, "secret-draft") || strings.Contains(raw, "Secret Draft") {
		t.Fatal("sitemap leaked a draft post")
	}
}

// TestPublicSitemapEmpty confirms a well-formed urlset when there are no
// published posts (homepage only, from site_url).
func TestPublicSitemapEmpty(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	resp := env.do(t, http.MethodGet, "/api/v1/sitemap.xml", nil)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("empty sitemap status = %d, want 200; body=%s", resp.StatusCode, body)
	}

	var doc struct {
		URLs []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("unmarshal empty sitemap: %v", err)
	}
	if len(doc.URLs) != 1 || doc.URLs[0].Loc != "https://example.com" {
		t.Fatalf("empty sitemap urls = %+v, want only homepage", doc.URLs)
	}
}

// TestPublicSitemapRespectsVisibility confirms scheduled-future and
// archived posts never appear in the sitemap.
func TestPublicSitemapRespectsVisibility(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	now := time.Now().UTC()

	pub := env.createPost(t, map[string]any{"title": "Live", "slug": "live"})
	env.publishPost(t, pub["id"].(string), now.Add(-time.Minute))

	sched := env.createPost(t, map[string]any{"title": "Future", "slug": "future"})
	future := now.Add(24 * time.Hour)
	env.setPostState(t, sched["id"].(string), "scheduled", &future)

	arch := env.createPost(t, map[string]any{"title": "Archived", "slug": "archived"})
	env.setPostState(t, arch["id"].(string), "archived", &now)

	resp := env.do(t, http.MethodGet, "/api/v1/sitemap.xml", nil)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read sitemap: %v", err)
	}
	raw := string(body)
	if !strings.Contains(raw, "https://example.com/live") {
		t.Fatalf("sitemap missing published post: %s", raw)
	}
	for _, leaked := range []string{"/future", "/archived"} {
		if strings.Contains(raw, leaked) {
			t.Fatalf("sitemap leaked %q: %s", leaked, raw)
		}
	}
}
