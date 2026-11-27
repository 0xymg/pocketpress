package content_test

import (
	"testing"

	"github.com/pocketpress/pocketpress/internal/content"
)

func TestNormalizeLocale(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"en", "en", true},
		{" EN ", "en", true},
		{"pt-BR", "pt-br", true},
		{"zh-Hant", "zh-hant", true},
		{"", "", false},
		{"e", "", false},
		{"!!!", "", false},
		{"en_US", "", false},
	}
	for _, tc := range cases {
		got, ok := content.NormalizeLocale(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("NormalizeLocale(%q) = %q,%v want %q,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestEnsureLocaleInList(t *testing.T) {
	got := content.EnsureLocaleInList([]string{"en"}, "tr")
	if len(got) != 2 || got[1] != "tr" {
		t.Fatalf("EnsureLocaleInList = %v, want [en tr]", got)
	}
	same := content.EnsureLocaleInList([]string{"en", "tr"}, "en")
	if len(same) != 2 {
		t.Fatalf("EnsureLocaleInList existing mutated length = %d", len(same))
	}
}
