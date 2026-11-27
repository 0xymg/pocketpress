package content

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

func TestImportGhostHappyPath(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()

	raw := []byte(`{
  "posts": [{
    "id": "1",
    "title": "From Ghost",
    "slug": "from-ghost",
    "html": "<h2>Section</h2><p>Hello <strong>world</strong>.</p>",
    "type": "post",
    "status": "published",
    "featured": false,
    "custom_excerpt": "Ghost excerpt",
    "published_at": "2024-05-01T12:00:00.000Z",
    "meta_title": "SEO",
    "canonical_url": "https://example.com/from-ghost"
  }],
  "tags": [{"id": "9", "name": "Updates", "slug": "updates"}],
  "posts_tags": [{"post_id": "1", "tag_id": "9"}]
}`)

	results, err := env.svc.ImportGhost(ctx, raw, env.authorID)
	if err != nil {
		t.Fatalf("ImportGhost() error = %v", err)
	}
	if len(results) != 1 || results[0].Post == nil || results[0].Error != nil {
		t.Fatalf("results = %+v", results)
	}
	p := results[0].Post
	if p.Row.Title != "From Ghost" || p.Row.Slug != "from-ghost" {
		t.Fatalf("title/slug = %q/%q", p.Row.Title, p.Row.Slug)
	}
	if p.Row.Status != store.PostStatusPublished {
		t.Fatalf("status = %q", p.Row.Status)
	}
	if p.Row.PublishedAt == nil || p.Row.PublishedAt.UTC().Format(time.RFC3339) != "2024-05-01T12:00:00Z" {
		t.Fatalf("published_at = %v", p.Row.PublishedAt)
	}
	if p.Row.Excerpt != "Ghost excerpt" || p.Row.SEOTitle != "SEO" {
		t.Fatalf("excerpt/seo = %q / %q", p.Row.Excerpt, p.Row.SEOTitle)
	}
	if len(p.Tags) != 1 || p.Tags[0].Name != "Updates" {
		t.Fatalf("tags = %+v", p.Tags)
	}
	if !strings.Contains(p.Row.ContentHTML, "<strong>") || p.Row.ContentJSON == "" || p.Row.ContentJSON == "{}" {
		t.Fatalf("content missing html=%q json=%q", p.Row.ContentHTML, p.Row.ContentJSON)
	}
	if !strings.Contains(p.Row.ContentJSON, `"type":"heading"`) {
		t.Fatalf("lexical missing heading: %s", p.Row.ContentJSON)
	}
	if len(env.events.published) != 1 {
		t.Fatalf("published events = %v", env.events.published)
	}
}

func TestImportGhostUsesLexicalWhenPresent(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()

	lexical := `{"root":{"children":[{"children":[{"detail":0,"format":0,"mode":"normal","style":"","text":"From lexical","type":"text","version":1}],"direction":"ltr","format":"","indent":0,"type":"paragraph","version":1}],"direction":"ltr","format":"","indent":0,"type":"root","version":1}}`
	payload := map[string]any{
		"posts": []map[string]any{{
			"id":      "1",
			"title":   "Lex",
			"slug":    "lex",
			"html":    "<p>From html</p>",
			"lexical": lexical,
			"type":    "post",
			"status":  "draft",
		}},
		"tags":       []any{},
		"posts_tags": []any{},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	results, err := env.svc.ImportGhost(ctx, raw, env.authorID)
	if err != nil || results[0].Post == nil {
		t.Fatalf("err=%v res=%+v", err, results)
	}
	if results[0].Post.Row.ContentJSON != lexical {
		t.Fatalf("content_json not preserved from Ghost lexical:\n%s", results[0].Post.Row.ContentJSON)
	}
}

func TestImportWordPressHappyPath(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()

	raw := []byte(`<?xml version="1.0" encoding="UTF-8" ?>
<rss version="2.0"
	xmlns:excerpt="http://wordpress.org/export/1.2/excerpt/"
	xmlns:content="http://purl.org/rss/1.0/modules/content/"
	xmlns:wp="http://wordpress.org/export/1.2/">
<channel>
	<item>
		<title>From WP</title>
		<content:encoded><![CDATA[<p>WP <em>body</em>.</p>]]></content:encoded>
		<excerpt:encoded><![CDATA[WP excerpt]]></excerpt:encoded>
		<wp:post_date_gmt>2023-06-10 09:00:00</wp:post_date_gmt>
		<wp:post_name>from-wp</wp:post_name>
		<wp:status>publish</wp:status>
		<wp:post_type>post</wp:post_type>
		<wp:is_sticky>0</wp:is_sticky>
		<category domain="category" nicename="news"><![CDATA[News]]></category>
		<category domain="post_tag" nicename="php"><![CDATA[PHP]]></category>
	</item>
</channel>
</rss>`)

	results, err := env.svc.ImportWordPress(ctx, raw, env.authorID)
	if err != nil {
		t.Fatalf("ImportWordPress() error = %v", err)
	}
	if len(results) != 1 || results[0].Post == nil {
		t.Fatalf("results = %+v", results)
	}
	p := results[0].Post
	if p.Row.Title != "From WP" || p.Row.Slug != "from-wp" {
		t.Fatalf("title/slug = %q/%q", p.Row.Title, p.Row.Slug)
	}
	if p.Row.Status != store.PostStatusPublished {
		t.Fatalf("status = %q", p.Row.Status)
	}
	if p.Row.PublishedAt == nil || p.Row.PublishedAt.UTC().Format(time.RFC3339) != "2023-06-10T09:00:00Z" {
		t.Fatalf("published_at = %v", p.Row.PublishedAt)
	}
	if p.Row.Excerpt != "WP excerpt" {
		t.Fatalf("excerpt = %q", p.Row.Excerpt)
	}
	if len(p.Categories) != 1 || p.Categories[0].Name != "News" {
		t.Fatalf("categories = %+v", p.Categories)
	}
	if len(p.Tags) != 1 || p.Tags[0].Name != "PHP" {
		t.Fatalf("tags = %+v", p.Tags)
	}
	if !strings.Contains(p.Row.ContentHTML, "<em>") {
		t.Fatalf("html = %s", p.Row.ContentHTML)
	}
	if !strings.Contains(p.Row.ContentJSON, "WP") {
		t.Fatalf("json = %s", p.Row.ContentJSON)
	}
}

func TestImportGhostInvalidFile(t *testing.T) {
	env := newServiceEnv(t)
	_, err := env.svc.ImportGhost(context.Background(), []byte(`not-json`), env.authorID)
	var ve *ValidationError
	if err == nil || !errors.As(err, &ve) || ve.Fields["file"] == "" {
		t.Fatalf("err = %v", err)
	}
}
