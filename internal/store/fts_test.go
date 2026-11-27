package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

func TestBuildFTSQuery(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"", "", false},
		{"   ", "", false},
		{"---", "", false},
		{`"OR" *`, `"OR"*`, true},
		{"Alpha", `"Alpha"*`, true},
		{"  hello   world ", `"hello"* "world"*`, true},
		{`hello"world`, `"helloworld"*`, true},
		{"search-tips", `"searchtips"*`, true},
		{"café", `"café"*`, true},
	}
	for _, tt := range tests {
		got, ok := store.BuildFTSQuery(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Errorf("BuildFTSQuery(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

// TestPostsFTSSearch covers body/title matching, prefix tokens, public
// visibility, punctuation-only queries, BM25 title ranking, and
// insert/update/delete index sync.
func TestPostsFTSSearch(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	author := createTestUser(t, st)

	guide, err := store.CreatePost(ctx, st.DB(), store.NewPost{
		Title:       "Alpha Guide",
		Slug:        "alpha-guide",
		Excerpt:     "intro notes",
		ContentText: "full text search rocks in production",
		Status:      store.PostStatusPublished,
		Author:      author.ID,
	})
	if err != nil {
		t.Fatalf("CreatePost(guide) error = %v", err)
	}
	tips, err := store.CreatePost(ctx, st.DB(), store.NewPost{
		Title:       "Search Tips",
		Slug:        "search-tips",
		ContentText: "alpha techniques for finding content",
		Status:      store.PostStatusPublished,
		Author:      author.ID,
	})
	if err != nil {
		t.Fatalf("CreatePost(tips) error = %v", err)
	}
	beta, err := store.CreatePost(ctx, st.DB(), store.NewPost{
		Title:       "Beta Notes",
		Slug:        "beta-notes",
		ContentText: "nothing special here",
		Status:      store.PostStatusDraft,
		Author:      author.ID,
	})
	if err != nil {
		t.Fatalf("CreatePost(beta) error = %v", err)
	}

	now := time.Now().UTC()
	for _, id := range []string{guide.ID, tips.ID} {
		if _, err := st.DB().ExecContext(ctx,
			`UPDATE posts SET published_at = ? WHERE id = ?`, now.Format(time.RFC3339), id,
		); err != nil {
			t.Fatalf("publish %s: %v", id, err)
		}
	}

	assertIDs := func(name string, filter store.PostFilter, ordered bool, want ...string) {
		t.Helper()
		rows, err := store.ListPosts(ctx, st.DB(), filter, store.PostSortUpdatedAt, true, 100, 0)
		if err != nil {
			t.Fatalf("%s ListPosts() error = %v", name, err)
		}
		ids := make([]string, len(rows))
		for i, p := range rows {
			ids[i] = p.ID
		}
		if len(ids) != len(want) {
			t.Fatalf("%s ListPosts() ids = %v, want %v", name, ids, want)
		}
		if ordered {
			for i := range want {
				if ids[i] != want[i] {
					t.Fatalf("%s ListPosts() ids = %v, want %v", name, ids, want)
				}
			}
		} else {
			set := map[string]bool{}
			for _, id := range ids {
				set[id] = true
			}
			for _, w := range want {
				if !set[w] {
					t.Fatalf("%s ListPosts() ids = %v, want to contain %v", name, ids, want)
				}
			}
		}
		n, err := store.CountPosts(ctx, st.DB(), filter)
		if err != nil {
			t.Fatalf("%s CountPosts() error = %v", name, err)
		}
		if n != len(want) {
			t.Fatalf("%s CountPosts() = %d, want %d", name, n, len(want))
		}
	}

	// Body match (not just title).
	assertIDs("body search", store.PostFilter{Search: "production"}, true, guide.ID)
	// Prefix: "alph" matches Alpha Guide title and "alpha" in tips body.
	// Order is BM25: title "Alpha" ranks above body "alpha".
	assertIDs("prefix alph", store.PostFilter{Search: "alph"}, true, guide.ID, tips.ID)
	assertIDs("title tips", store.PostFilter{Search: "tips"}, true, tips.ID)
	assertIDs("case", store.PostFilter{Search: "SEARCH"}, true, tips.ID, guide.ID)
	assertIDs("draft body", store.PostFilter{Search: "special"}, true, beta.ID)
	assertIDs("punctuation", store.PostFilter{Search: `***`}, true)

	// Public visibility: draft excluded.
	assertIDs("public search", store.PostFilter{
		Status:          store.PostStatusPublished,
		PublishedBefore: &now,
		Search:          "search",
	}, true, tips.ID, guide.ID)

	// Sync on update.
	if err := store.UpdatePost(ctx, st.DB(), beta.ID, store.PostUpdate{
		Title:       "Beta Notes",
		Slug:        "beta-notes",
		ContentText: "now mentions production explicitly",
		Status:      store.PostStatusDraft,
		Author:      author.ID,
	}); err != nil {
		t.Fatalf("UpdatePost(beta) error = %v", err)
	}
	assertIDs("after update", store.PostFilter{Search: "production"}, false, guide.ID, beta.ID)

	// Sync on delete.
	if err := store.DeletePost(ctx, st.DB(), guide.ID); err != nil {
		t.Fatalf("DeletePost: %v", err)
	}
	assertIDs("after delete", store.PostFilter{Search: "production"}, true, beta.ID)
}
