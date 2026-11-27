package content

import (
	"regexp"
	"strings"
)

// localePattern accepts a simplified BCP 47 language tag: a 2–3 letter
// primary language subtag, optionally followed by further 2–8 character
// subtags (script/region/variant), all lowercase after NormalizeLocale.
// Examples: "en", "tr", "pt-br", "zh-hant".
var localePattern = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{2,8})*$`)

// NormalizeLocale trims, lowercases, and validates a locale tag. Returns
// ok=false for blank or malformed input.
func NormalizeLocale(s string) (normalized string, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || !localePattern.MatchString(s) {
		return "", false
	}
	return s, true
}

// LocaleInList reports whether locale is present in the allowlist.
func LocaleInList(locale string, list []string) bool {
	for _, item := range list {
		if item == locale {
			return true
		}
	}
	return false
}

// EnsureLocaleInList returns list with locale appended when missing.
// The returned slice is a new copy when mutation is needed.
func EnsureLocaleInList(list []string, locale string) []string {
	if LocaleInList(locale, list) {
		return list
	}
	out := make([]string, len(list), len(list)+1)
	copy(out, list)
	return append(out, locale)
}
