package http

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func TestImportGhostJSON(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	body := `{
  "posts": [{
    "id": "1",
    "title": "Ghost Import",
    "slug": "ghost-import",
    "html": "<p>Hello <strong>Ghost</strong>.</p>",
    "type": "post",
    "status": "draft"
  }],
  "tags": [],
  "posts_tags": []
}`
	req, err := http.NewRequest(http.MethodPost, env.srv.URL+"/api/admin/v1/posts/import/ghost", strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := env.client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; body = %s", resp.StatusCode, raw)
	}
	imported, _ := m["imported"].([]any)
	failed, _ := m["failed"].([]any)
	if len(imported) != 1 || len(failed) != 0 {
		t.Fatalf("imported/failed = %v / %v; body = %s", imported, failed, raw)
	}
	row, _ := imported[0].(map[string]any)
	post, _ := row["post"].(map[string]any)
	if post["title"] != "Ghost Import" {
		t.Fatalf("title = %v", post["title"])
	}
	html, _ := post["content_html"].(string)
	if !strings.Contains(html, "<strong>") {
		t.Fatalf("content_html = %s", html)
	}
}

func TestImportGhostMultipart(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	payload := `{"posts":[{"id":"1","title":"From File","slug":"from-file","html":"<p>Hi</p>","type":"post","status":"draft"}],"tags":[],"posts_tags":[]}`
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", "ghost-export.json")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	_, _ = io.WriteString(part, payload)
	_ = w.Close()

	req, err := http.NewRequest(http.MethodPost, env.srv.URL+"/api/admin/v1/posts/import/ghost", &buf)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := env.client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; body = %s", resp.StatusCode, raw)
	}
	imported, _ := m["imported"].([]any)
	if len(imported) != 1 {
		t.Fatalf("imported = %v", imported)
	}
	post, _ := imported[0].(map[string]any)["post"].(map[string]any)
	if post["title"] != "From File" {
		t.Fatalf("title = %v", post["title"])
	}
}

func TestImportWordPressMultipart(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	wxr := `<?xml version="1.0" encoding="UTF-8" ?>
<rss version="2.0"
	xmlns:excerpt="http://wordpress.org/export/1.2/excerpt/"
	xmlns:content="http://purl.org/rss/1.0/modules/content/"
	xmlns:wp="http://wordpress.org/export/1.2/">
<channel>
	<item>
		<title>WP Import</title>
		<content:encoded><![CDATA[<p>Hello <em>WP</em>.</p>]]></content:encoded>
		<wp:post_name>wp-import</wp:post_name>
		<wp:status>publish</wp:status>
		<wp:post_type>post</wp:post_type>
		<wp:post_date_gmt>2024-01-01 00:00:00</wp:post_date_gmt>
	</item>
</channel>
</rss>`

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", "wordpress.xml")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	_, _ = io.WriteString(part, wxr)
	_ = w.Close()

	req, err := http.NewRequest(http.MethodPost, env.srv.URL+"/api/admin/v1/posts/import/wordpress", &buf)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := env.client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; body = %s", resp.StatusCode, raw)
	}
	imported, _ := m["imported"].([]any)
	failed, _ := m["failed"].([]any)
	if len(imported) != 1 || len(failed) != 0 {
		t.Fatalf("imported/failed = %v / %v; body = %s", imported, failed, raw)
	}
	post, _ := imported[0].(map[string]any)["post"].(map[string]any)
	if post["title"] != "WP Import" || post["status"] != "published" {
		t.Fatalf("post = %v", post)
	}
}

func TestImportGhostUnauthorized(t *testing.T) {
	env := newTestEnv(t, false)
	req, err := http.NewRequest(http.MethodPost, env.srv.URL+"/api/admin/v1/posts/import/ghost", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := env.client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		raw, _ := readJSON(t, resp)
		t.Fatalf("status = %d, want 401; body = %s", resp.StatusCode, raw)
	}
	_ = resp.Body.Close()
}

func TestImportGhostRejectsBadMultipartExt(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", "export.txt")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	_, _ = io.WriteString(part, `{}`)
	_ = w.Close()

	req, err := http.NewRequest(http.MethodPost, env.srv.URL+"/api/admin/v1/posts/import/ghost", &buf)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := env.client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, raw)
	}
	if m["error"].(map[string]any)["code"] != "validation_failed" {
		t.Fatalf("body = %s", raw)
	}
}
