package htmlconv

import (
	"strings"
	"testing"
)

func TestConvertBasicHTML(t *testing.T) {
	html := `<h2>Hello</h2><p>A <strong>bold</strong> and <em>italic</em> <a href="https://example.com">link</a>.</p>` +
		`<ul><li>One</li><li>Two</li></ul>` +
		`<ol><li>First</li></ol>` +
		`<blockquote><p>Quote</p></blockquote>` +
		`<pre><code class="language-go">fmt.Println("hi")</code></pre>` +
		`<hr>` +
		`<p><img src="/api/files/1/x.png" alt="pic"></p>`

	got, err := Convert(html)
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if got.ContentHTML != html {
		t.Fatalf("ContentHTML mismatch")
	}
	if !strings.Contains(got.ContentJSON, `"type":"heading"`) {
		t.Fatalf("missing heading: %s", got.ContentJSON)
	}
	if !strings.Contains(got.ContentJSON, `"type":"list"`) {
		t.Fatalf("missing list: %s", got.ContentJSON)
	}
	if !strings.Contains(got.ContentJSON, `"type":"quote"`) {
		t.Fatalf("missing quote: %s", got.ContentJSON)
	}
	if !strings.Contains(got.ContentJSON, `"type":"code"`) {
		t.Fatalf("missing code: %s", got.ContentJSON)
	}
	if !strings.Contains(got.ContentJSON, `"type":"horizontalrule"`) {
		t.Fatalf("missing hr: %s", got.ContentJSON)
	}
	if !strings.Contains(got.ContentJSON, `"type":"image"`) {
		t.Fatalf("missing image: %s", got.ContentJSON)
	}
	if !strings.Contains(got.ContentJSON, `"type":"link"`) {
		t.Fatalf("missing link: %s", got.ContentJSON)
	}
	if !strings.Contains(got.ContentJSON, "bold") {
		t.Fatalf("missing bold text: %s", got.ContentJSON)
	}
}

func TestConvertEmpty(t *testing.T) {
	got, err := Convert("  ")
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if got.ContentHTML != "" {
		t.Fatalf("HTML = %q", got.ContentHTML)
	}
	if !strings.Contains(got.ContentJSON, `"type":"root"`) {
		t.Fatalf("json = %s", got.ContentJSON)
	}
}

func TestConvertSkipsTables(t *testing.T) {
	got, err := Convert(`<p>Before</p><table><tr><td>cell</td></tr></table><p>After</p>`)
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if strings.Contains(got.ContentJSON, "cell") {
		t.Fatalf("table cell leaked into lexical: %s", got.ContentJSON)
	}
	if !strings.Contains(got.ContentJSON, "Before") || !strings.Contains(got.ContentJSON, "After") {
		t.Fatalf("json = %s", got.ContentJSON)
	}
}

func TestValidLexicalJSON(t *testing.T) {
	if ValidLexicalJSON("") || ValidLexicalJSON("{}") || ValidLexicalJSON(`{"foo":1}`) {
		t.Fatal("expected false for empty/invalid")
	}
	ok := `{"root":{"children":[],"direction":"ltr","format":"","indent":0,"type":"root","version":1}}`
	if !ValidLexicalJSON(ok) {
		t.Fatal("expected true for valid root")
	}
}
