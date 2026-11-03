package content

import (
	"strings"
	"testing"
)

// TestSlugify is the table of frozen slug-normalization behavior: Turkish
// and accented letters, symbol/emoji collapsing, hyphen trimming, empty
// results and the 200-character cap.
func TestSlugify(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"simple", "Hello World", "hello-world"},
		{"already a slug", "already-good-slug", "already-good-slug"},
		{"leading and trailing hyphens", "-leading-and-trailing-", "leading-and-trailing"},
		{"collapse whitespace runs", "  Multiple   Spaces  ", "multiple-spaces"},
		{"symbols become one hyphen", "Special!@#$%Chars", "special-chars"},
		{"underscores are separators", "snake_case_name", "snake-case-name"},

		// Turkish letters: the dotless-i family plus ş/ğ/ç/ö/ü.
		{"turkish istanbul", "İstanbul Şehri", "istanbul-sehri"},
		{"turkish urun cesitleri", "Ürün Çeşitleri", "urun-cesitleri"},
		{"turkish full alphabet", "Ğğ İı Şş Çç Öö Üü", "gg-ii-ss-cc-oo-uu"},
		{"dotless i lowercase", "ışık", "isik"},

		// Common accented / special Latin letters via NFKD + transliteration.
		{"french accents", "café résumé", "cafe-resume"},
		{"naive diaeresis", "naïve", "naive"},
		{"german eszett", "Straße", "strasse"},
		{"n with tilde", "Mañana", "manana"},
		{"slashed o", "Bløb", "blob"},
		{"ae ligature", "Encyclopædia", "encyclopaedia"},

		// Emoji and other non-sluggable runes collapse away.
		{"emoji in middle", "Hello 😀 World", "hello-world"},
		{"emoji only", "😀😀😀", ""},

		// Empty results.
		{"empty", "", ""},
		{"only symbols", "!!!", ""},
		{"only whitespace", "   ", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Slugify(tc.in)
			if got != tc.want {
				t.Fatalf("Slugify(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSlugifyCapsAt200 confirms the length cap and that a cut never leaves a
// trailing hyphen.
func TestSlugifyCapsAt200(t *testing.T) {
	// A long unbroken run is truncated to exactly 200 characters.
	long := Slugify(strings.Repeat("a", 250))
	if len(long) != MaxSlugLen {
		t.Fatalf("len(Slugify(250 'a')) = %d, want %d", len(long), MaxSlugLen)
	}
	if long != strings.Repeat("a", MaxSlugLen) {
		t.Fatalf("Slugify(250 'a') = %q, want 200 'a'", long)
	}

	// When the cut lands on a hyphen it is trimmed: 199 'a', a separator, then
	// filler — the 200th character would be the hyphen, so it is removed.
	trimmed := Slugify(strings.Repeat("a", 199) + " " + strings.Repeat("b", 50))
	if len(trimmed) > MaxSlugLen {
		t.Fatalf("len = %d, want <= %d", len(trimmed), MaxSlugLen)
	}
	if strings.HasSuffix(trimmed, "-") {
		t.Fatalf("Slugify result %q has a trailing hyphen after truncation", trimmed)
	}
	if trimmed != strings.Repeat("a", 199) {
		t.Fatalf("Slugify(199a + space + 50b) = %q, want 199 'a'", trimmed)
	}
}
