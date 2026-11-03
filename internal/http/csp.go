package http

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"strings"

	"github.com/pocketpress/pocketpress/admin"
)

// baseSPACSPDirectives is the Content-Security-Policy applied to the admin SPA,
// minus the script-src source list (which is completed at runtime with the
// hashes of any inline bootstrap scripts the build emits). It matches the
// development plan's Phase 8 policy: same-origin everything, inline styles
// allowed (the SPA ships a small amount of inline CSS), images from anywhere
// (posts embed remote cover/OG images), no plugins, no framing.
var baseSPACSPDirectives = []string{
	"default-src 'self'",
	// script-src is inserted separately (see buildSPACSP) so its computed
	// hashes land in the right place.
	"style-src 'self' 'unsafe-inline'",
	"img-src 'self' data: https: http:",
	"connect-src 'self'",
	"font-src 'self'",
	"object-src 'none'",
	"base-uri 'self'",
	"frame-ancestors 'none'",
}

// buildSPACSP assembles the admin SPA Content-Security-Policy from the embedded
// build output. SvelteKit's static index.html contains an inline module-loader
// bootstrap script whose exact bytes change on every admin rebuild (it embeds
// the hashed chunk filenames), so a hardcoded hash would silently break the SPA
// after a rebuild. Instead the sha256 of every inline <script> in the embedded
// index.html is computed here at startup and added to script-src, keeping the
// policy in lockstep with whatever the current build actually ships.
//
// A failure to read the embedded assets yields a strict script-src 'self'
// policy (the base directives) — safe by default; the caller has already logged
// the asset-load failure and swapped in an error SPA handler.
func buildSPACSP() string {
	scriptSrc := "script-src 'self'"
	if hashes, err := inlineScriptHashesFromEmbed(); err == nil {
		for _, h := range hashes {
			scriptSrc += " " + h
		}
	}

	directives := make([]string, 0, len(baseSPACSPDirectives)+1)
	directives = append(directives, baseSPACSPDirectives[0], scriptSrc)
	directives = append(directives, baseSPACSPDirectives[1:]...)
	return strings.Join(directives, "; ")
}

// inlineScriptHashesFromEmbed reads the embedded admin index.html and returns
// the CSP source tokens ('sha256-...') for its inline scripts.
func inlineScriptHashesFromEmbed() ([]string, error) {
	distFS, err := admin.FS()
	if err != nil {
		return nil, err
	}
	index, err := fs.ReadFile(distFS, "index.html")
	if err != nil {
		return nil, err
	}
	return inlineScriptHashes(index), nil
}

// inlineScriptHashes finds every inline <script> (a <script> element with no
// src attribute) in html and returns a CSP 'sha256-<base64>' token for each,
// hashed over the exact byte content between the element's tags — which is
// precisely what a browser hashes when enforcing script-src. Scripts with a
// src attribute are external (already covered by 'self') and are skipped.
func inlineScriptHashes(html []byte) []string {
	var tokens []string
	s := string(html)
	lower := strings.ToLower(s)

	for i := 0; i < len(s); {
		open := strings.Index(lower[i:], "<script")
		if open < 0 {
			break
		}
		open += i

		tagEnd := strings.IndexByte(s[open:], '>')
		if tagEnd < 0 {
			break
		}
		tagEnd += open // index of the '>' closing the opening tag

		closeRel := strings.Index(lower[tagEnd+1:], "</script>")
		if closeRel < 0 {
			break
		}
		contentStart := tagEnd + 1
		contentEnd := contentStart + closeRel

		openTag := s[open : tagEnd+1]
		if !hasSrcAttr(openTag) {
			content := s[contentStart:contentEnd]
			sum := sha256.Sum256([]byte(content))
			tokens = append(tokens, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
		}

		i = contentEnd + len("</script>")
	}
	return tokens
}

// hasSrcAttr reports whether a <script ...> opening tag carries a src
// attribute.
func hasSrcAttr(openTag string) bool {
	return strings.Contains(strings.ToLower(openTag), "src=")
}
