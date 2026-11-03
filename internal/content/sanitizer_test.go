package content

import (
	"strings"
	"testing"
)

// TestSanitizeAllowedMarkup confirms the allowlisted elements and attributes
// survive sanitization unchanged.
func TestSanitizeAllowedMarkup(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"paragraph and inline emphasis", `<p>Hello <strong>world</strong> and <em>friends</em></p>`, `<p>Hello <strong>world</strong> and <em>friends</em></p>`},
		{"all inline marks", `<b>b</b><i>i</i><u>u</u><s>s</s><code>c</code>`, `<b>b</b><i>i</i><u>u</u><s>s</s><code>c</code>`},
		{"subscript and superscript", `<p>H<sub>2</sub>O and x<sup>2</sup></p>`, `<p>H<sub>2</sub>O and x<sup>2</sup></p>`},
		{"headings", `<h1>Title</h1><h2>Sub</h2><h3>x</h3><h4>x</h4><h5>x</h5><h6>x</h6>`, `<h1>Title</h1><h2>Sub</h2><h3>x</h3><h4>x</h4><h5>x</h5><h6>x</h6>`},
		{"unordered list", `<ul><li>one</li><li>two</li></ul>`, `<ul><li>one</li><li>two</li></ul>`},
		{"ordered list", `<ol><li>x</li></ol>`, `<ol><li>x</li></ol>`},
		{"blockquote", `<blockquote><p>q</p></blockquote>`, `<blockquote><p>q</p></blockquote>`},
		{"code block", `<pre><code>go build ./...</code></pre>`, `<pre><code>go build ./...</code></pre>`},
		{"line break and rule", `<p>a<br>b</p><hr>`, `<p>a<br>b</p><hr>`},
		{"absolute http link", `<a href="http://example.com">link</a>`, `<a href="http://example.com">link</a>`},
		{"https link", `<a href="https://example.com/x">link</a>`, `<a href="https://example.com/x">link</a>`},
		{"relative link", `<a href="/relative/path">rel</a>`, `<a href="/relative/path">rel</a>`},
		{"mailto link", `<a href="mailto:x@y.com">mail</a>`, `<a href="mailto:x@y.com">mail</a>`},
		{"image with alt and dims", `<img src="/api/files/a.png" alt="pic" width="100" height="50">`, `<img src="/api/files/a.png" alt="pic" width="100" height="50">`},
		{"relative image src", `<img src="x.png" alt="a">`, `<img src="x.png" alt="a">`},
		{"empty input", ``, ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Sanitize(tc.in); got != tc.want {
				t.Fatalf("Sanitize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSanitizeStripsAttacks is the hard policy test: every injection vector
// must be neutralized. Each case lists substrings that must NOT appear in the
// output and (optionally) legitimate text that must survive.
func TestSanitizeStripsAttacks(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		absent  []string // must not appear anywhere in the output
		present []string // must appear (legit content preserved)
	}{
		{
			name:    "script tag and content removed",
			in:      `<script>alert('xss')</script><p>safe</p>`,
			absent:  []string{"script", "alert"},
			present: []string{"<p>safe</p>"},
		},
		{
			name:   "img onerror handler stripped, src kept",
			in:     `<img src="x" onerror="alert(1)">`,
			absent: []string{"onerror", "alert"},
			// The relative src survives; the event handler is gone.
			present: []string{`<img src="x">`},
		},
		{
			name:    "javascript href killed",
			in:      `<a href="javascript:alert(1)">click</a>`,
			absent:  []string{"javascript", "alert", "href"},
			present: []string{"click"},
		},
		{
			name:   "vbscript href killed",
			in:     `<a href="vbscript:msgbox(1)">x</a>`,
			absent: []string{"vbscript", "href"},
		},
		{
			name:   "data uri image src killed",
			in:     `<img src="data:image/png;base64,AAAA" alt="a">`,
			absent: []string{"data:", "base64"},
			// The img element survives but with no src; alt is kept.
			present: []string{`alt="a"`},
		},
		{
			name:   "data uri href killed",
			in:     `<a href="data:text/html,<script>alert(1)</script>">x</a>`,
			absent: []string{"data:", "script", "href"},
		},
		{
			name:    "nested disallowed iframe removed",
			in:      `<p>ok<iframe src="http://evil.com"></iframe></p>`,
			absent:  []string{"iframe", "evil.com"},
			present: []string{"<p>ok</p>"},
		},
		{
			name:    "style element and attribute stripped",
			in:      `<style>body{display:none}</style><p style="color:red" class="c" id="i" data-x="y">styled</p>`,
			absent:  []string{"<style", "display:none", "style=", "class=", "id=", "data-x"},
			present: []string{"<p>styled</p>"},
		},
		{
			name:    "div and span unwrapped to text",
			in:      `<div><span>unwrap</span></div>`,
			absent:  []string{"<div", "<span"},
			present: []string{"unwrap"},
		},
		{
			name:    "table stripped, cell text preserved",
			in:      `<table><tr><td>cell</td></tr></table>`,
			absent:  []string{"<table", "<tr", "<td"},
			present: []string{"cell"},
		},
		{
			name:   "link target attribute stripped",
			in:     `<a href="http://x" target="_blank" rel="noopener">t</a>`,
			absent: []string{"target", "rel="},
			// href survives; target/rel do not.
			present: []string{`<a href="http://x">t</a>`},
		},
		{
			name:    "non-integer image dimensions dropped",
			in:      `<img src="/a.png" width="10%" height="abc">`,
			absent:  []string{"width", "height", "%", "abc"},
			present: []string{`<img src="/a.png">`},
		},
		{
			name:    "malformed unclosed tags do not break sanitizer",
			in:      `<p>unclosed <b>bold <script>alert(1)`,
			absent:  []string{"script", "alert"},
			present: []string{"unclosed", "bold"},
		},
		{
			name:    "svg with embedded script removed",
			in:      `<svg><script>alert(1)</script></svg><p>after</p>`,
			absent:  []string{"svg", "script", "alert"},
			present: []string{"<p>after</p>"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Sanitize(tc.in)
			lower := strings.ToLower(got)
			for _, bad := range tc.absent {
				if strings.Contains(lower, strings.ToLower(bad)) {
					t.Fatalf("Sanitize(%q) = %q, must not contain %q", tc.in, got, bad)
				}
			}
			for _, good := range tc.present {
				if !strings.Contains(got, good) {
					t.Fatalf("Sanitize(%q) = %q, want it to contain %q", tc.in, got, good)
				}
			}
		})
	}
}

// TestSanitizeIsIdempotent confirms sanitizing already-sanitized HTML is a
// no-op, which the update path relies on when it re-sanitizes stored HTML.
func TestSanitizeIsIdempotent(t *testing.T) {
	inputs := []string{
		`<p>Hello <strong>world</strong></p>`,
		`<a href="http://example.com">link</a>`,
		`<ul><li>one</li><li>two</li></ul>`,
		`<img src="/api/files/a.png" alt="pic" width="100" height="50">`,
	}
	for _, in := range inputs {
		once := Sanitize(in)
		twice := Sanitize(once)
		if once != twice {
			t.Fatalf("Sanitize not idempotent for %q: once=%q twice=%q", in, once, twice)
		}
	}
}
