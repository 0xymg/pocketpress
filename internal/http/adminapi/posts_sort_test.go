package adminapi

import (
	"testing"

	"github.com/pocketpress/pocketpress/internal/store"
)

// TestParsePostSort locks in the admin post-list sort allowlist: the default
// and both accepted fields (updated_at, title) in both directions, and the
// fallback for unknown values.
func TestParsePostSort(t *testing.T) {
	cases := []struct {
		in       string
		field    store.PostSortField
		wantDesc bool
	}{
		{"", store.PostSortUpdatedAt, true},             // default: newest first
		{"-updated_at", store.PostSortUpdatedAt, true},  // explicit default
		{"updated_at", store.PostSortUpdatedAt, false},  // oldest first
		{"title", store.PostSortTitle, false},           // A→Z
		{"-title", store.PostSortTitle, true},           // Z→A
		{"bogus", store.PostSortUpdatedAt, true},        // unknown → default
		{"published_at", store.PostSortUpdatedAt, true}, // not allowlisted → default
	}
	for _, c := range cases {
		field, desc := parsePostSort(c.in)
		if field != c.field || desc != c.wantDesc {
			t.Errorf("parsePostSort(%q) = (%v, %v), want (%v, %v)", c.in, field, desc, c.field, c.wantDesc)
		}
	}
}
