package store

import (
	"strings"
	"unicode"
)

// BuildFTSQuery turns a free-text search string into a safe FTS5 MATCH
// expression. Each whitespace-separated token is reduced to letters and
// numbers (unicode-aware), then emitted as a quoted prefix term ("token"*)
// so partial words still match and FTS operators (OR/AND/NOT) cannot be
// injected. Tokens are ANDed (FTS5 default).
//
// Returns ("", false) when the input yields no usable tokens — callers
// should treat that as "match nothing" rather than an unfiltered list, so
// a query of only punctuation does not dump the whole catalog.
func BuildFTSQuery(raw string) (string, bool) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return "", false
	}

	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		token := ftsToken(field)
		if token == "" {
			continue
		}
		// Quoted form keeps reserved words (OR/AND/NOT) from being
		// parsed as operators; the trailing * keeps prefix matching.
		parts = append(parts, `"`+token+`"*`)
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, " "), true
}

// ftsToken keeps letters and numbers from s (lowercasing ASCII via the
// FTS tokenizer's own case folding; we do not lower here). Everything
// else — quotes, operators, punctuation — is stripped so user input
// cannot inject FTS5 MATCH syntax.
func ftsToken(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
