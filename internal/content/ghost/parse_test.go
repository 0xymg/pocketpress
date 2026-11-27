package ghost

import (
	"strings"
	"testing"
	"time"
)

func TestParseFullExport(t *testing.T) {
	raw := []byte(`{
  "db": [{
    "meta": {"exported_on": 1700000000000, "version": "5.0.0"},
    "data": {
      "posts": [
        {
          "id": "1",
          "title": "Hello Ghost",
          "slug": "hello-ghost",
          "html": "<p>Body <strong>here</strong>.</p>",
          "lexical": "",
          "type": "post",
          "status": "published",
          "featured": true,
          "custom_excerpt": "Short",
          "published_at": "2024-01-15T10:00:00.000Z",
          "meta_title": "SEO Hello",
          "meta_description": "SEO desc",
          "canonical_url": "https://example.com/hello-ghost"
        },
        {
          "id": "2",
          "title": "A Page",
          "slug": "a-page",
          "html": "<p>Page</p>",
          "type": "page",
          "status": "published"
        },
        {
          "id": "3",
          "title": "Drafty",
          "slug": "drafty",
          "html": "<p>Draft</p>",
          "type": "post",
          "status": "draft"
        }
      ],
      "tags": [
        {"id": "10", "name": "News", "slug": "news"},
        {"id": "11", "name": "Go", "slug": "go"}
      ],
      "posts_tags": [
        {"post_id": "1", "tag_id": "10"},
        {"post_id": "1", "tag_id": "11"}
      ]
    }
  }]
}`)

	docs, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("len = %d, want 2 (page skipped)", len(docs))
	}
	d := docs[0]
	if d.Title != "Hello Ghost" || d.Slug != "hello-ghost" {
		t.Fatalf("doc0 = %+v", d)
	}
	if d.Status != "published" || d.PublishedAt == nil {
		t.Fatalf("status/published_at = %s / %v", d.Status, d.PublishedAt)
	}
	if d.PublishedAt.UTC().Format(time.RFC3339) != "2024-01-15T10:00:00Z" {
		t.Fatalf("published_at = %v", d.PublishedAt)
	}
	if !d.Featured || d.Excerpt != "Short" {
		t.Fatalf("featured/excerpt = %v / %q", d.Featured, d.Excerpt)
	}
	if d.SEOTitle != "SEO Hello" || d.SEODescription != "SEO desc" {
		t.Fatalf("seo = %q / %q", d.SEOTitle, d.SEODescription)
	}
	if d.CanonicalURL != "https://example.com/hello-ghost" {
		t.Fatalf("canonical = %q", d.CanonicalURL)
	}
	if len(d.Tags) != 2 {
		t.Fatalf("tags = %v", d.Tags)
	}
	if docs[1].Status != "draft" || docs[1].Title != "Drafty" {
		t.Fatalf("doc1 = %+v", docs[1])
	}
}

func TestParseBareData(t *testing.T) {
	raw := []byte(`{
  "posts": [{"id": 7, "title": "Bare", "slug": "bare", "html": "<p>Hi</p>", "status": "published", "type": "post"}],
  "tags": [],
  "posts_tags": []
}`)
	docs, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(docs) != 1 || docs[0].Title != "Bare" || docs[0].SourceKey != "bare" {
		t.Fatalf("docs = %+v", docs)
	}
}

func TestParseRejectsEmptyAndInvalid(t *testing.T) {
	if _, err := Parse(nil); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty err = %v", err)
	}
	if _, err := Parse([]byte(`{not json`)); err == nil {
		t.Fatal("expected invalid JSON error")
	}
	if _, err := Parse([]byte(`{"db":[{"data":{"posts":[]}}]}`)); err == nil {
		t.Fatal("expected no posts error")
	}
}

func TestParseScheduledBecomesDraft(t *testing.T) {
	raw := []byte(`{"posts":[{"id":"1","title":"Later","slug":"later","html":"<p>x</p>","type":"post","status":"scheduled"}],"tags":[],"posts_tags":[]}`)
	docs, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if docs[0].Status != "draft" {
		t.Fatalf("status = %q", docs[0].Status)
	}
}
