package wordpress

import (
	"strings"
	"testing"
	"time"
)

const sampleWXR = `<?xml version="1.0" encoding="UTF-8" ?>
<rss version="2.0"
	xmlns:excerpt="http://wordpress.org/export/1.2/excerpt/"
	xmlns:content="http://purl.org/rss/1.0/modules/content/"
	xmlns:wp="http://wordpress.org/export/1.2/">
<channel>
	<title>Demo</title>
	<item>
		<title>Hello WP</title>
		<link>https://example.com/hello-wp/</link>
		<content:encoded><![CDATA[<p>Hello <strong>world</strong>.</p>]]></content:encoded>
		<excerpt:encoded><![CDATA[A short excerpt]]></excerpt:encoded>
		<wp:post_id>10</wp:post_id>
		<wp:post_date>2024-02-01 08:30:00</wp:post_date>
		<wp:post_date_gmt>2024-02-01 08:30:00</wp:post_date_gmt>
		<wp:post_name>hello-wp</wp:post_name>
		<wp:status>publish</wp:status>
		<wp:post_type>post</wp:post_type>
		<wp:is_sticky>1</wp:is_sticky>
		<category domain="category" nicename="news"><![CDATA[News]]></category>
		<category domain="post_tag" nicename="go"><![CDATA[Go]]></category>
	</item>
	<item>
		<title>A Page</title>
		<content:encoded><![CDATA[<p>Page body</p>]]></content:encoded>
		<wp:post_name>a-page</wp:post_name>
		<wp:status>publish</wp:status>
		<wp:post_type>page</wp:post_type>
	</item>
	<item>
		<title>Draft Post</title>
		<content:encoded><![CDATA[<p>Draft body</p>]]></content:encoded>
		<wp:post_name>draft-post</wp:post_name>
		<wp:status>draft</wp:status>
		<wp:post_type>post</wp:post_type>
		<wp:is_sticky>0</wp:is_sticky>
	</item>
	<item>
		<title>Trashed</title>
		<content:encoded><![CDATA[<p>Nope</p>]]></content:encoded>
		<wp:post_name>trashed</wp:post_name>
		<wp:status>trash</wp:status>
		<wp:post_type>post</wp:post_type>
	</item>
</channel>
</rss>`

func TestParseWXR(t *testing.T) {
	docs, err := Parse([]byte(sampleWXR))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("len = %d, want 2 (page+trash skipped)", len(docs))
	}
	d := docs[0]
	if d.Title != "Hello WP" || d.Slug != "hello-wp" {
		t.Fatalf("doc0 = %+v", d)
	}
	if d.Status != "published" {
		t.Fatalf("status = %q", d.Status)
	}
	if d.PublishedAt == nil || d.PublishedAt.UTC().Format(time.RFC3339) != "2024-02-01T08:30:00Z" {
		t.Fatalf("published_at = %v", d.PublishedAt)
	}
	if !d.Featured || d.Excerpt != "A short excerpt" {
		t.Fatalf("featured/excerpt = %v / %q", d.Featured, d.Excerpt)
	}
	if !strings.Contains(d.HTML, "<strong>world</strong>") {
		t.Fatalf("html = %q", d.HTML)
	}
	if len(d.Categories) != 1 || d.Categories[0] != "News" {
		t.Fatalf("categories = %v", d.Categories)
	}
	if len(d.Tags) != 1 || d.Tags[0] != "Go" {
		t.Fatalf("tags = %v", d.Tags)
	}
	if docs[1].Title != "Draft Post" || docs[1].Status != "draft" {
		t.Fatalf("doc1 = %+v", docs[1])
	}
}

func TestParseRejectsEmpty(t *testing.T) {
	if _, err := Parse(nil); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("err = %v", err)
	}
	if _, err := Parse([]byte(`<rss><channel></channel></rss>`)); err == nil {
		t.Fatal("expected no posts error")
	}
}
