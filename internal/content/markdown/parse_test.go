package markdown

import (
	"strings"
	"testing"
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
