package content

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

func TestImportMarkdownHappyPath(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()

	md := "---\n" +
		"title: Imported Post\n" +
		"slug: imported-post\n" +
		"tags: [Go]\n" +
		"categories: [Updates]\n" +
		"excerpt: From markdown\n" +
		"---\n\n" +
		"## Section\n\nHello **world**.\n"

	results, err := env.svc.ImportMarkdown(ctx, []MarkdownImportItem{{
		Filename: "imported-post.md",
		Markdown: md,
	}}, env.authorID)
	if err != nil {
		t.Fatalf("ImportMarkdown() error = %v", err)
	}
	if len(results) != 1 || results[0].Post == nil || results[0].Error != nil {
		t.Fatalf("results = %+v", results)
	}
	p := results[0].Post
	if p.Row.Title != "Imported Post" || p.Row.Slug != "imported-post" {
		t.Fatalf("post title/slug = %q/%q", p.Row.Title, p.Row.Slug)
	}
	if p.Row.Status != store.PostStatusDraft {
		t.Fatalf("status = %q, want draft", p.Row.Status)
	}
	if p.Row.Excerpt != "From markdown" {
		t.Fatalf("excerpt = %q", p.Row.Excerpt)
	}
	if len(p.Categories) != 1 || p.Categories[0].Name != "Updates" {
		t.Fatalf("categories = %+v", p.Categories)
	}
	if len(p.Tags) != 1 || p.Tags[0].Name != "Go" {
		t.Fatalf("tags = %+v", p.Tags)
	}
	if p.Row.ContentHTML == "" || p.Row.ContentJSON == "" || p.Row.ContentJSON == "{}" {
		t.Fatalf("content missing: html=%q json=%q", p.Row.ContentHTML, p.Row.ContentJSON)
	}
	if !strings.Contains(p.Row.ContentHTML, "<h2>") || !strings.Contains(p.Row.ContentHTML, "<strong>") {
		t.Fatalf("content_html = %s", p.Row.ContentHTML)
	}
}

func TestImportMarkdownPublish(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()

	results, err := env.svc.ImportMarkdown(ctx, []MarkdownImportItem{{
		Filename: "live.md",
		Markdown: "---\ntitle: Live\nstatus: published\n---\n\nBody.\n",
	}}, env.authorID)
	if err != nil {
		t.Fatalf("ImportMarkdown() error = %v", err)
	}
	if results[0].Post == nil || results[0].Post.Row.Status != store.PostStatusPublished {
		t.Fatalf("want published, got %+v", results[0])
	}
	if results[0].Post.Row.PublishedAt == nil {
		t.Fatal("published_at is nil")
	}
	if len(env.events.published) != 1 {
		t.Fatalf("published events = %v, want 1", env.events.published)
	}
}

func TestImportMarkdownFrontmatterAliasesAndDate(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()

	md := "---\n" +
		"title: Aliased Import\n" +
		"permalink: /posts/aliased-import/\n" +
		"description: From description\n" +
		"draft: false\n" +
		"tag: Alpha\n" +
		"category: Beta\n" +
		"meta_title: SEO Title\n" +
		"canonical: https://example.com/aliased-import\n" +
		"date: 2024-03-01T12:00:00Z\n" +
		"---\n\nHello body.\n"

	results, err := env.svc.ImportMarkdown(ctx, []MarkdownImportItem{{
		Filename: "aliased.md",
		Markdown: md,
	}}, env.authorID)
	if err != nil {
		t.Fatalf("ImportMarkdown() error = %v", err)
	}
	if results[0].Error != nil || results[0].Post == nil {
		t.Fatalf("result = %+v", results[0])
	}
	p := results[0].Post
	if p.Row.Slug != "aliased-import" {
		t.Fatalf("slug = %q", p.Row.Slug)
	}
	if p.Row.Excerpt != "From description" {
		t.Fatalf("excerpt = %q", p.Row.Excerpt)
	}
	if p.Row.SEOTitle != "SEO Title" {
		t.Fatalf("seo_title = %q", p.Row.SEOTitle)
	}
	if p.Row.SEODescription != "From description" {
		t.Fatalf("seo_description = %q", p.Row.SEODescription)
	}
	if p.Row.CanonicalURL != "https://example.com/aliased-import" {
		t.Fatalf("canonical_url = %q", p.Row.CanonicalURL)
	}
	if p.Row.Status != store.PostStatusPublished {
		t.Fatalf("status = %q", p.Row.Status)
	}
	if p.Row.PublishedAt == nil || p.Row.PublishedAt.UTC().Format(time.RFC3339) != "2024-03-01T12:00:00Z" {
		t.Fatalf("published_at = %v, want 2024-03-01T12:00:00Z", p.Row.PublishedAt)
	}
	if len(p.Tags) != 1 || p.Tags[0].Name != "Alpha" {
		t.Fatalf("tags = %+v", p.Tags)
	}
	if len(p.Categories) != 1 || p.Categories[0].Name != "Beta" {
		t.Fatalf("categories = %+v", p.Categories)
	}
}

func TestImportMarkdownPartialFailure(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()

	results, err := env.svc.ImportMarkdown(ctx, []MarkdownImportItem{
		{Filename: "ok.md", Markdown: "# Good\n\nOk.\n"},
		{Filename: "bad.md", Markdown: "---\nstatus: scheduled\n---\n\nNope.\n"},
	}, env.authorID)
	if err != nil {
		t.Fatalf("ImportMarkdown() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d", len(results))
	}
	if results[0].Post == nil || results[0].Post.Row.Title != "Good" {
		t.Fatalf("first = %+v", results[0])
	}
	if results[1].Error == nil || results[1].Error.Fields["status"] == "" {
		t.Fatalf("second should fail status validation: %+v", results[1])
	}
}

func TestImportMarkdownEmptyRejected(t *testing.T) {
	env := newServiceEnv(t)
	_, err := env.svc.ImportMarkdown(context.Background(), nil, env.authorID)
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Fields["documents"] == "" {
		t.Fatalf("err = %v", err)
	}
}

func TestImportMarkdownReusesCategoriesAndTags(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()

	_, err := env.svc.CreateCategory(ctx, CategoryInput{Name: "News"})
	if err != nil {
		t.Fatalf("CreateCategory() error = %v", err)
	}
	_, err = env.svc.CreateTag(ctx, TagInput{Name: "Go"})
	if err != nil {
		t.Fatalf("CreateTag() error = %v", err)
	}

	results, err := env.svc.ImportMarkdown(ctx, []MarkdownImportItem{{
		Filename: "x.md",
		Markdown: "---\ntitle: X\ncategories: [News]\ntags: [Go]\n---\n\nHi.\n",
	}}, env.authorID)
	if err != nil || results[0].Post == nil {
		t.Fatalf("import failed: err=%v res=%+v", err, results)
	}

	cats, err := env.svc.ListCategories(ctx)
	if err != nil {
		t.Fatalf("ListCategories() error = %v", err)
	}
	if len(cats) != 1 {
		t.Fatalf("categories count = %d, want 1 (reused)", len(cats))
	}
	tags, err := env.svc.ListTags(ctx)
	if err != nil {
		t.Fatalf("ListTags() error = %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("tags count = %d, want 1 (reused)", len(tags))
	}
}
