package http

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func TestImportMarkdownJSON(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	resp := env.do(t, http.MethodPost, "/api/admin/v1/posts/import/markdown", map[string]any{
		"documents": []map[string]any{{
			"filename": "hello.md",
			"markdown": "---\ntitle: Hello Import\ntags: [Go]\n---\n\n## Hi\n\nBody **here**.\n",
		}},
	})
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
	if post["title"] != "Hello Import" {
		t.Fatalf("title = %v", post["title"])
	}
	if post["status"] != "draft" {
		t.Fatalf("status = %v", post["status"])
	}
	html, _ := post["content_html"].(string)
	if !strings.Contains(html, "<h2>") || !strings.Contains(html, "<strong>") {
		t.Fatalf("content_html = %s", html)
	}
	jsonBody, _ := post["content_json"].(string)
	if jsonBody == "" || jsonBody == "{}" {
		t.Fatalf("content_json missing: %q", jsonBody)
	}
}

func TestImportMarkdownMultipart(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("files", "from-file.md")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	_, _ = io.WriteString(part, "# From File\n\nParagraph.\n")
	_ = w.Close()

	req, err := http.NewRequest(http.MethodPost, env.srv.URL+"/api/admin/v1/posts/import/markdown", &buf)
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
	row, _ := imported[0].(map[string]any)
	post, _ := row["post"].(map[string]any)
	if post["title"] != "From File" {
		t.Fatalf("title = %v, want From File", post["title"])
	}
}

func TestImportMarkdownUnauthorized(t *testing.T) {
	env := newTestEnv(t, false)
	// No setup/login — jar is empty.
	resp := env.do(t, http.MethodPost, "/api/admin/v1/posts/import/markdown", map[string]any{
		"documents": []map[string]any{{"filename": "x.md", "markdown": "Hi"}},
	})
	if resp.StatusCode != http.StatusUnauthorized {
		raw, _ := readJSON(t, resp)
		t.Fatalf("status = %d, want 401; body = %s", resp.StatusCode, raw)
	}
	_ = resp.Body.Close()
}

func TestImportMarkdownPartialAndPublish(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	resp := env.do(t, http.MethodPost, "/api/admin/v1/posts/import/markdown", map[string]any{
		"documents": []map[string]any{
			{
				"filename": "live.md",
				"markdown": "---\ntitle: Live One\nstatus: published\n---\n\nLive body.\n",
			},
			{
				"filename": "bad.md",
				"markdown": "---\nstatus: archived\n---\n\nNope.\n",
			},
		},
	})
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; body = %s", resp.StatusCode, raw)
	}
	imported, _ := m["imported"].([]any)
	failed, _ := m["failed"].([]any)
	if len(imported) != 1 || len(failed) != 1 {
		t.Fatalf("imported/failed lens = %d/%d; body = %s", len(imported), len(failed), raw)
	}
	post, _ := imported[0].(map[string]any)["post"].(map[string]any)
	if post["status"] != "published" {
		t.Fatalf("imported status = %v, want published", post["status"])
	}
	fail, _ := failed[0].(map[string]any)
	if fail["code"] != "validation_failed" {
		t.Fatalf("failed entry = %v", fail)
	}
}

func TestImportMarkdownRejectsNonMarkdownMultipart(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("files", "notes.txt")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	_, _ = io.WriteString(part, "not markdown ext")
	_ = w.Close()

	req, err := http.NewRequest(http.MethodPost, env.srv.URL+"/api/admin/v1/posts/import/markdown", &buf)
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
