package webhook

import (
	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/store"
)

// MatchesFilters reports whether post satisfies the webhook's category/tag
// filters. Empty filter lists match any post. Within a non-empty list, any
// listed id is enough (OR); category and tag dimensions AND together.
//
// A nil post never matches a non-empty filter (defensive; dispatch always
// passes a real aggregate).
func MatchesFilters(f store.WebhookFilters, post *content.Post) bool {
	f = f.Normalize()
	if len(f.CategoryIDs) == 0 && len(f.TagIDs) == 0 {
		return true
	}
	if post == nil {
		return false
	}
	if len(f.CategoryIDs) > 0 && !intersectsIDs(f.CategoryIDs, categoryIDs(post.Categories)) {
		return false
	}
	if len(f.TagIDs) > 0 && !intersectsIDs(f.TagIDs, tagIDs(post.Tags)) {
		return false
	}
	return true
}

func categoryIDs(cats []store.Category) []string {
	out := make([]string, len(cats))
	for i, c := range cats {
		out[i] = c.ID
	}
	return out
}

func tagIDs(tags []store.Tag) []string {
	out := make([]string, len(tags))
	for i, t := range tags {
		out[i] = t.ID
	}
	return out
}

func intersectsIDs(want, have []string) bool {
	if len(want) == 0 || len(have) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(have))
	for _, id := range have {
		set[id] = struct{}{}
	}
	for _, id := range want {
		if _, ok := set[id]; ok {
			return true
		}
	}
	return false
}
