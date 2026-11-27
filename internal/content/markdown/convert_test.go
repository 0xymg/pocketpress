package markdown

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConvertBasicBlocks(t *testing.T) {
	body := "## Heading\n\n" +
		"A paragraph with **bold** and *italic* and `code`.\n\n" +
		"- item one\n- item two\n\n" +
		"1. first\n2. second\n\n" +
		"> a quote\n\n" +
		"```go\nfmt.Println(\"hi\")\n```\n\n" +
		"---\n\n" +
		"[link](https://example.com) and ![alt](/api/files/1/x.png)\n"

	got, err := Convert(body)
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if got.ContentJSON == "" || got.ContentJSON == "{}" {
		t.Fatalf("ContentJSON empty: %q", got.ContentJSON)
	}
	if !json.Valid([]byte(got.ContentJSON)) {
		t.Fatalf("ContentJSON not valid JSON: %s", got.ContentJSON)
	}
	if !strings.Contains(got.ContentHTML, "<h2>") {
		t.Fatalf("HTML missing h2: %s", got.ContentHTML)
	}
	if !strings.Contains(got.ContentHTML, "<strong>") && !strings.Contains(got.ContentHTML, "<b>") {
		t.Fatalf("HTML missing bold: %s", got.ContentHTML)
	}
	if !strings.Contains(got.ContentHTML, "<ul>") || !strings.Contains(got.ContentHTML, "<ol>") {
		t.Fatalf("HTML missing lists: %s", got.ContentHTML)
	}
	if !strings.Contains(got.ContentHTML, "<blockquote>") {
		t.Fatalf("HTML missing blockquote: %s", got.ContentHTML)
	}
	if !strings.Contains(got.ContentHTML, "<pre>") {
		t.Fatalf("HTML missing pre: %s", got.ContentHTML)
	}
	if !strings.Contains(got.ContentHTML, "<hr") {
		t.Fatalf("HTML missing hr: %s", got.ContentHTML)
	}
	if !strings.Contains(got.ContentHTML, `href="https://example.com"`) {
		t.Fatalf("HTML missing link: %s", got.ContentHTML)
	}
	if !strings.Contains(got.ContentHTML, `src="/api/files/1/x.png"`) {
		t.Fatalf("HTML missing image: %s", got.ContentHTML)
	}

	var state map[string]any
	if err := json.Unmarshal([]byte(got.ContentJSON), &state); err != nil {
		t.Fatalf("unmarshal lexical: %v", err)
	}
	root, _ := state["root"].(map[string]any)
	children, _ := root["children"].([]any)
	if len(children) < 5 {
		t.Fatalf("lexical children = %d, want several blocks; json=%s", len(children), got.ContentJSON)
	}

	types := map[string]bool{}
	for _, c := range children {
		m, _ := c.(map[string]any)
		if typ, _ := m["type"].(string); typ != "" {
			types[typ] = true
		}
	}
	for _, want := range []string{"heading", "paragraph", "list", "quote", "code", "horizontalrule"} {
		if !types[want] {
			t.Fatalf("lexical missing %s node; types=%v json=%s", want, types, got.ContentJSON)
		}
	}
}

func TestConvertEmpty(t *testing.T) {
	got, err := Convert("   ")
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if !json.Valid([]byte(got.ContentJSON)) {
		t.Fatalf("invalid json: %s", got.ContentJSON)
	}
	if got.ContentHTML != "" {
		t.Fatalf("HTML = %q, want empty", got.ContentHTML)
	}
}

func TestConvertStrikethrough(t *testing.T) {
	got, err := Convert("This is ~~gone~~.\n")
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if !strings.Contains(got.ContentHTML, "<del>") && !strings.Contains(got.ContentHTML, "<s>") {
		t.Fatalf("HTML missing strikethrough: %s", got.ContentHTML)
	}
}
