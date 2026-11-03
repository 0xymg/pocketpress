package content

import "github.com/microcosm-cc/bluemonday"

// sanitizePolicy is PocketPress's frozen HTML allowlist. It is built once and
// is safe for concurrent Sanitize calls.
//
// The editor (Lexical, client side) exports HTML that the server never
// trusts: it is filtered here to a small, fixed set of formatting elements
// and attributes before it is stored or served. Everything not explicitly
// allowed — script/iframe/style/span/div/table, class/style/id/data-*
// attributes, dangerous URL schemes — is stripped.
var sanitizePolicy = buildSanitizePolicy()

// buildSanitizePolicy constructs the allowlist policy. See the package-level
// comment on sanitizePolicy for the contract.
func buildSanitizePolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()

	// Attribute-free formatting elements: paragraphs and breaks, headings,
	// lists, block quotes, code blocks, inline code, and inline emphasis.
	// A bare <span>/<div> is not allowed, so bluemonday unwraps it to its
	// text children; disallowed elements like <script>/<iframe> are removed
	// with their content.
	p.AllowElements(
		"p", "br", "hr",
		"h1", "h2", "h3", "h4", "h5", "h6",
		"ul", "ol", "li",
		"blockquote", "pre", "code",
		"strong", "b", "em", "i", "u", "s", "sub", "sup",
	)

	// Links carry href only. No target attribute is permitted (it is simply
	// not in the allowlist and is stripped), so links open in the same tab
	// and there is no window.opener tab-nabbing surface; consequently no rel
	// is forced. The URL policy below drops javascript:/data:/vbscript: hrefs.
	p.AllowAttrs("href").OnElements("a")

	// Images carry src/alt plus optional integer width/height. The URL policy
	// below constrains src to http(s) or a relative path (e.g. an uploaded
	// /api/files/... URL); data: URIs are rejected.
	p.AllowAttrs("src", "alt").OnElements("img")
	p.AllowAttrs("width", "height").Matching(bluemonday.Integer).OnElements("img")

	// URL policy shared by <a href> and <img src>: only http, https and
	// mailto schemes, plus relative URLs. RequireParseableURLs drops anything
	// net/url cannot parse; the scheme allowlist drops javascript:, data: and
	// every other scheme. mailto on an <img src> is meaningless but harmless
	// (no browser will load it) and never emitted by the editor.
	p.AllowURLSchemes("http", "https", "mailto")
	p.AllowRelativeURLs(true)
	p.RequireParseableURLs(true)

	return p
}

// Sanitize filters raw HTML down to PocketPress's frozen allowlist (see
// sanitizePolicy). The result is safe to store as posts.content_html and to
// serve to clients. Empty input returns an empty string.
func Sanitize(raw string) string {
	if raw == "" {
		return ""
	}
	return sanitizePolicy.Sanitize(raw)
}
