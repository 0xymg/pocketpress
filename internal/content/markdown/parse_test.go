package markdown

import (
	"strings"
	"testing"
	"time"
)

func TestParseFrontmatter(t *testing.T) {
	raw := "---\n" +
		"title: Hello World\n" +
		"slug: hello-world\n" +
		"excerpt: A short blurb\n" +
		"status: published\n" +
		"featured: true\n" +
		"tags: [Go, Blog]\n" +
		"categories: News\n" +
		"seo_title: SEO\n" +
		"seo_description: Desc\n" +
		"canonical_url: https://example.com/hello\n" +
		"---\n\n" +
		"# Ignored H1\n\nBody paragraph.\n"

	doc, err := Parse("ignored.md", raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if doc.Meta.Title != "Hello World" {
		t.Fatalf("title = %q", doc.Meta.Title)
	}
	if doc.Meta.Slug != "hello-world" {
		t.Fatalf("slug = %q", doc.Meta.Slug)
	}
	if doc.Meta.Status != "published" || !doc.Meta.Featured {
		t.Fatalf("status/featured = %q/%v", doc.Meta.Status, doc.Meta.Featured)
	}
	if len(doc.Meta.Tags) != 2 || doc.Meta.Tags[0] != "Go" {
		t.Fatalf("tags = %#v", doc.Meta.Tags)
	}
	if len(doc.Meta.Categories) != 1 || doc.Meta.Categories[0] != "News" {
		t.Fatalf("categories = %#v", doc.Meta.Categories)
	}
	if doc.DefaultTitle() != "Hello World" {
		t.Fatalf("DefaultTitle() = %q", doc.DefaultTitle())
	}
	if !strings.Contains(doc.BodyWithoutLeadingH1(), "# Ignored H1") {
		t.Fatalf("frontmatter title should keep body H1; body = %q", doc.BodyWithoutLeadingH1())
	}
}

func TestParseFrontmatterAliases(t *testing.T) {
	raw := "---\n" +
		"title: Aliased\n" +
		"permalink: /blog/aliased-post/\n" +
		"description: Shared description\n" +
		"draft: false\n" +
		"feature: true\n" +
		"tag: Go, Rust\n" +
		"category: [Updates]\n" +
		"meta_title: Meta Title\n" +
		"og_description: OG Desc\n" +
		"canonical: https://example.com/aliased\n" +
		"date: 2024-06-15T10:30:00Z\n" +
		"---\n\nBody.\n"

	doc, err := Parse("x.md", raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	m := doc.Meta
	if m.Slug != "aliased-post" {
		t.Fatalf("slug from permalink = %q", m.Slug)
	}
	if m.Excerpt != "Shared description" {
		t.Fatalf("excerpt from description = %q", m.Excerpt)
	}
	if m.SEODescription != "OG Desc" {
		// og_description wins over description for SEO when both present —
		// wait, we set SEODescription from seo aliases first, then description
		// only fills if empty. og_description is an alias so should be "OG Desc".
		t.Fatalf("seo_description = %q, want OG Desc", m.SEODescription)
	}
	if m.SEOTitle != "Meta Title" {
		t.Fatalf("seo_title = %q", m.SEOTitle)
	}
	if m.CanonicalURL != "https://example.com/aliased" {
		t.Fatalf("canonical = %q", m.CanonicalURL)
	}
	if m.Status != "published" {
		t.Fatalf("status from draft:false = %q", m.Status)
	}
	if !m.Featured {
		t.Fatal("featured from feature: true")
	}
	if len(m.Tags) != 2 || m.Tags[0] != "Go" || m.Tags[1] != "Rust" {
		t.Fatalf("tags = %#v", m.Tags)
	}
	if len(m.Categories) != 1 || m.Categories[0] != "Updates" {
		t.Fatalf("categories = %#v", m.Categories)
	}
	if m.PublishedAt == nil || !m.PublishedAt.Equal(time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("published_at = %v", m.PublishedAt)
	}
	if m.ResolvedStatus() != "published" {
		t.Fatalf("ResolvedStatus() = %q", m.ResolvedStatus())
	}
}

func TestParseDraftTrue(t *testing.T) {
	doc, err := Parse("d.md", "---\ntitle: Drafty\ndraft: true\n---\n\nHi.\n")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if doc.Meta.Status != "draft" {
		t.Fatalf("status = %q, want draft", doc.Meta.Status)
	}
}

func TestParsePublishedBool(t *testing.T) {
	doc, err := Parse("p.md", "---\ntitle: Live\npublished: true\n---\n\nHi.\n")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if doc.Meta.Status != "published" {
		t.Fatalf("status = %q", doc.Meta.Status)
	}
	if doc.Meta.PublishedAt != nil {
		t.Fatalf("published_at should be nil for bool published, got %v", doc.Meta.PublishedAt)
	}
}

func TestParseDateImpliesPublished(t *testing.T) {
	doc, err := Parse("d.md", "---\ntitle: Dated\ndate: 2023-01-02\n---\n\nHi.\n")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if doc.Meta.Status != "published" {
		t.Fatalf("status = %q, want published", doc.Meta.Status)
	}
	if doc.Meta.PublishedAt == nil || doc.Meta.PublishedAt.Format("2006-01-02") != "2023-01-02" {
		t.Fatalf("published_at = %v", doc.Meta.PublishedAt)
	}
}

func TestParseDescriptionFillsSEOWhenNoSEOKey(t *testing.T) {
	doc, err := Parse("d.md", "---\ntitle: X\ndescription: Only desc\n---\n\nHi.\n")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if doc.Meta.Excerpt != "Only desc" || doc.Meta.SEODescription != "Only desc" {
		t.Fatalf("excerpt/seo = %q/%q", doc.Meta.Excerpt, doc.Meta.SEODescription)
	}
}

func TestParseStatusAliases(t *testing.T) {
	for _, tc := range []struct {
		raw, want string
	}{
		{"status: publish", "published"},
		{"status: public", "published"},
		{"status: live", "published"},
		{"status: unpublished", "draft"},
	} {
		doc, err := Parse("s.md", "---\ntitle: S\n"+tc.raw+"\n---\n\nHi.\n")
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", tc.raw, err)
		}
		if doc.Meta.Status != tc.want {
			t.Fatalf("%s → status = %q, want %q", tc.raw, doc.Meta.Status, tc.want)
		}
	}
}

func TestParsePermalinkURL(t *testing.T) {
	doc, err := Parse("x.md", "---\ntitle: X\nurl: https://example.com/posts/my-slug.html\n---\n\nHi.\n")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if doc.Meta.Slug != "my-slug" {
		t.Fatalf("slug = %q", doc.Meta.Slug)
	}
}

func TestParseLeadingH1Title(t *testing.T) {
	doc, err := Parse("my-post.md", "# Ship it\n\nParagraph.\n")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if doc.DefaultTitle() != "Ship it" {
		t.Fatalf("DefaultTitle() = %q, want Ship it", doc.DefaultTitle())
	}
	if doc.BodyWithoutLeadingH1() != "Paragraph." {
		t.Fatalf("body = %q, want Paragraph.", doc.BodyWithoutLeadingH1())
	}
}

func TestParseFilenameTitle(t *testing.T) {
	doc, err := Parse("announcing_pocketpress.md", "Just a paragraph.\n")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if doc.DefaultTitle() != "announcing pocketpress" {
		t.Fatalf("DefaultTitle() = %q", doc.DefaultTitle())
	}
}

func TestParseInvalidYAML(t *testing.T) {
	_, err := Parse("x.md", "---\ntitle: [unterminated\n---\nBody\n")
	if err == nil {
		t.Fatal("Parse() error = nil, want invalid YAML")
	}
}

func TestParseNoFrontmatter(t *testing.T) {
	doc, err := Parse("", "Hello **world**.\n")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if doc.Meta.Title != "" || doc.Body != "Hello **world**." {
		t.Fatalf("doc = %+v", doc)
	}
	if doc.DefaultTitle() != "Untitled" {
		t.Fatalf("DefaultTitle() = %q, want Untitled", doc.DefaultTitle())
	}
}

func TestParseRejectsHugeDocument(t *testing.T) {
	raw := strings.Repeat("a", MaxDocumentBytes+1)
	_, err := Parse("big.md", raw)
	if err == nil {
		t.Fatal("Parse() error = nil, want size error")
	}
}
