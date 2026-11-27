package webhook

import (
	"testing"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/store"
)

func TestMatchesFilters(t *testing.T) {
	catA := store.Category{ID: "cat-a", Name: "A", Slug: "a"}
	catB := store.Category{ID: "cat-b", Name: "B", Slug: "b"}
	tagX := store.Tag{ID: "tag-x", Name: "X", Slug: "x"}
	tagY := store.Tag{ID: "tag-y", Name: "Y", Slug: "y"}

	post := &content.Post{
		Row:        store.Post{ID: "p1", Slug: "hello"},
		Categories: []store.Category{catA},
		Tags:       []store.Tag{tagX},
	}
	emptyPost := &content.Post{Row: store.Post{ID: "p2", Slug: "bare"}}

	cases := []struct {
		name string
		f    store.WebhookFilters
		post *content.Post
		want bool
	}{
		{"empty filters match any", store.WebhookFilters{}, post, true},
		{"empty filters match bare post", store.WebhookFilters{}, emptyPost, true},
		{"category match", store.WebhookFilters{CategoryIDs: []string{"cat-a"}}, post, true},
		{"category miss", store.WebhookFilters{CategoryIDs: []string{"cat-b"}}, post, false},
		{"category OR", store.WebhookFilters{CategoryIDs: []string{"cat-b", "cat-a"}}, post, true},
		{"tag match", store.WebhookFilters{TagIDs: []string{"tag-x"}}, post, true},
		{"tag miss", store.WebhookFilters{TagIDs: []string{"tag-y"}}, post, false},
		{"category AND tag both match", store.WebhookFilters{CategoryIDs: []string{"cat-a"}, TagIDs: []string{"tag-x"}}, post, true},
		{"category match tag miss", store.WebhookFilters{CategoryIDs: []string{"cat-a"}, TagIDs: []string{"tag-y"}}, post, false},
		{"category miss tag match", store.WebhookFilters{CategoryIDs: []string{"cat-b"}, TagIDs: []string{"tag-x"}}, post, false},
		{"filters on bare post fail", store.WebhookFilters{CategoryIDs: []string{"cat-a"}}, emptyPost, false},
		{"nil post with filters fails", store.WebhookFilters{CategoryIDs: []string{"cat-a"}}, nil, false},
		{"nil post with empty filters matches", store.WebhookFilters{}, nil, true},
		{"multi-cat post OR", store.WebhookFilters{CategoryIDs: []string{"cat-b"}}, &content.Post{
			Categories: []store.Category{catA, catB}, Tags: []store.Tag{tagY},
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MatchesFilters(tc.f, tc.post); got != tc.want {
				t.Fatalf("MatchesFilters(%+v) = %v, want %v", tc.f, got, tc.want)
			}
		})
	}
}
