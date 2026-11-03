// Package content holds PocketPress's post/category/tag domain layer: slug
// normalization, the transitional plain-text-to-HTML derivation used before
// the Lexical editor arrives, and the service that orchestrates validation,
// slug rules, revisions and persistence on top of internal/store.
package content

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// MaxSlugLen caps a normalized slug's length, per the frozen slug rules.
const MaxSlugLen = 200

// transliterations maps letters that Unicode NFKD does NOT decompose to an
// ASCII base — chiefly the Turkish dotless-i family and a handful of Latin
// letters with strokes or ligatures — to their ASCII equivalents. Letters
// that DO decompose under NFKD (ç, ö, ü, ş, ğ, ä, é, ñ, …) are handled by
// the NFKD pass plus combining-mark stripping below, so they are omitted
// here on purpose.
var transliterations = map[rune]string{
	'ı': "i", 'İ': "i",
	'ş': "s", 'Ş': "s",
	'ğ': "g", 'Ğ': "g",
	'ß': "ss",
	'æ': "ae", 'Æ': "ae",
	'œ': "oe", 'Œ': "oe",
	'ø': "o", 'Ø': "o",
	'đ': "d", 'Đ': "d",
	'ð': "d", 'Ð': "d",
	'ł': "l", 'Ł': "l",
	'þ': "th", 'Þ': "th",
}

// Slugify normalizes s into PocketPress's canonical slug form:
//
//   - apply the explicit transliterations above,
//   - NFKD-decompose and drop combining marks (so accented Latin letters
//     collapse to their base: é→e, ñ→n, ü→u, …),
//   - lowercase,
//   - replace every run of characters outside [a-z0-9] with a single '-',
//   - trim leading/trailing '-',
//   - cap the result at MaxSlugLen characters.
//
// It is a pure function. An input that contains no sluggable characters
// (empty, whitespace, emoji-only, …) returns "", which callers treat as a
// validation error.
func Slugify(s string) string {
	// 1. Explicit transliterations for letters NFKD will not decompose.
	var pre strings.Builder
	for _, r := range s {
		if repl, ok := transliterations[r]; ok {
			pre.WriteString(repl)
		} else {
			pre.WriteRune(r)
		}
	}

	// 2. NFKD-decompose, then drop combining marks (accents).
	decomposed := norm.NFKD.String(pre.String())
	var stripped strings.Builder
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		stripped.WriteRune(r)
	}

	// 3. Lowercase and collapse non-[a-z0-9] runs into single hyphens.
	lower := strings.ToLower(stripped.String())
	var out strings.Builder
	prevHyphen := false
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
			prevHyphen = false
			continue
		}
		if !prevHyphen {
			out.WriteByte('-')
			prevHyphen = true
		}
	}

	slug := strings.Trim(out.String(), "-")

	// 4. Cap length; a slug is pure ASCII here so bytes == characters.
	if len(slug) > MaxSlugLen {
		slug = strings.TrimRight(slug[:MaxSlugLen], "-")
	}
	return slug
}
