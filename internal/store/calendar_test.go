package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

func TestListCalendarPosts(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	author := createTestUser(t, st)

	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 11, 30, 23, 59, 59, 0, time.UTC)

	mk := func(title, slug, status string) store.Post {
		t.Helper()
		p, err := store.CreatePost(ctx, st.DB(), store.NewPost{
			Title: title, Slug: slug, Status: store.PostStatusDraft, Author: author.ID,
		})
		if err != nil {
			t.Fatalf("CreatePost(%s): %v", slug, err)
		}
		switch status {
		case store.PostStatusScheduled:
			when := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
			if err := store.SetPostStatus(ctx, st.DB(), p.ID, store.PostStatusScheduled, nil, &when); err != nil {
				t.Fatalf("schedule %s: %v", slug, err)
			}
		case store.PostStatusPublished:
			when := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
			if err := store.SetPostStatus(ctx, st.DB(), p.ID, store.PostStatusPublished, &when, nil); err != nil {
				t.Fatalf("publish %s: %v", slug, err)
			}
		case store.PostStatusDraft:
			// leave as draft
		}
		got, err := store.GetPostByID(ctx, st.DB(), p.ID)
		if err != nil {
			t.Fatalf("GetPostByID(%s): %v", slug, err)
		}
		return got
	}

	scheduled := mk("Soon", "soon", store.PostStatusScheduled)
	published := mk("Live", "live", store.PostStatusPublished)
	_ = mk("Draft", "drafty", store.PostStatusDraft)

	// Outside the November window.
	out, err := store.CreatePost(ctx, st.DB(), store.NewPost{
		Title: "December", Slug: "dec", Status: store.PostStatusDraft, Author: author.ID,
	})
	if err != nil {
		t.Fatalf("CreatePost(dec): %v", err)
	}
	decWhen := time.Date(2026, 12, 5, 12, 0, 0, 0, time.UTC)
	if err := store.SetPostStatus(ctx, st.DB(), out.ID, store.PostStatusScheduled, nil, &decWhen); err != nil {
		t.Fatalf("schedule dec: %v", err)
	}

	rows, err := store.ListCalendarPosts(ctx, st.DB(), from, to)
	if err != nil {
		t.Fatalf("ListCalendarPosts: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ListCalendarPosts len = %d, want 2 (scheduled+published in range)", len(rows))
	}
	// Published (Nov 10) then scheduled (Nov 15).
	if rows[0].ID != published.ID || rows[1].ID != scheduled.ID {
		t.Fatalf("order = [%s, %s], want [%s, %s]", rows[0].Slug, rows[1].Slug, published.Slug, scheduled.Slug)
	}

	// Empty window.
	emptyFrom := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	emptyTo := time.Date(2026, 1, 31, 23, 59, 59, 0, time.UTC)
	rows, err = store.ListCalendarPosts(ctx, st.DB(), emptyFrom, emptyTo)
	if err != nil {
		t.Fatalf("ListCalendarPosts empty: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("empty window len = %d, want 0", len(rows))
	}
}
