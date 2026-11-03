package content

import "testing"

// TestHTMLToText covers the plain-text derivation: tag stripping, whitespace
// collapsing, paragraph breaks as "\n\n", <br> as a space, entity decoding
// and empty input.
func TestHTMLToText(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"single paragraph", `<p>Hello world</p>`, "Hello world"},
		{"inline marks flattened", `<p>Hello <strong>bold</strong> and <em>italic</em></p>`, "Hello bold and italic"},
		{"two paragraphs", `<p>First</p><p>Second</p>`, "First\n\nSecond"},
		{"headings separate", `<h1>Title</h1><h2>Sub</h2>`, "Title\n\nSub"},
		{"list items separate", `<ul><li>one</li><li>two</li></ul>`, "one\n\ntwo"},
		{"blockquote nested paragraph", `<blockquote><p>quote</p></blockquote>`, "quote"},
		{"pre code preserved as text", `<pre><code>go build ./...</code></pre>`, "go build ./..."},
		{"br becomes space", `<p>Line 1<br>Line 2</p>`, "Line 1 Line 2"},
		{"whitespace collapsed", "<p>a   \n   b\t c</p>", "a b c"},
		{"entities decoded", `<p>A &amp; B &lt;tag&gt;</p>`, "A & B <tag>"},
		{"link text kept", `<p>see <a href="http://x">this</a></p>`, "see this"},
		{"empty", ``, ""},
		{"whitespace only", `   `, ""},
		{"tags only no text", `<img src="/a.png"><hr>`, ""},
		{"leading trailing space trimmed", `<p>  spaced  </p>`, "spaced"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HTMLToText(tc.in); got != tc.want {
				t.Fatalf("HTMLToText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestHTMLToTextFromSanitized confirms the end-to-end Sanitize→HTMLToText
// pipeline: an attack-laden document yields clean, break-preserving text with
// no markup or payload.
func TestHTMLToTextFromSanitized(t *testing.T) {
	in := `<h1>Title</h1><script>alert(1)</script><p>Body <strong>one</strong></p>` +
		`<ul><li>a</li><li>b</li></ul><p>Link <a href="javascript:alert(1)">x</a></p>`
	want := "Title\n\nBody one\n\na\n\nb\n\nLink x"
	if got := HTMLToText(Sanitize(in)); got != want {
		t.Fatalf("HTMLToText(Sanitize(%q)) = %q, want %q", in, got, want)
	}
}
