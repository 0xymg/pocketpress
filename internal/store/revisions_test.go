package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/pocketpress/pocketpress/internal/store"
)

// TestRevisionCreateAndLatest confirms CreateRevision + LatestRevision, and
// that LatestRevision reports ErrNotFound before any revision exists.
func TestRevisionCreateAndLatest(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	author := createTestUser(t, st)
	post, err := store.CreatePost(ctx, st.DB(), store.NewPost{Title: "P", Slug: "p", Status: store.PostStatusDraft, Author: author.ID})
	if err != nil {
		t.Fatalf("CreatePost() error = %v", err)
	}

	if _, err := store.LatestRevision(ctx, st.DB(), post.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("LatestRevision() with no revisions error = %v, want ErrNotFound", err)
	}

	if _, err := store.CreateRevision(ctx, st.DB(), store.NewRevision{
		PostID: post.ID, Title: "first", Excerpt: "e1", ContentHTML: "<p>1</p>", EditorID: author.ID,
	}); err != nil {
		t.Fatalf("CreateRevision(first) error = %v", err)
	}
	if _, err := store.CreateRevision(ctx, st.DB(), store.NewRevision{
		PostID: post.ID, Title: "second", Excerpt: "e2", ContentJSON: `{"v":2}`, ContentHTML: "<p>2</p>", EditorID: author.ID,
	}); err != nil {
		t.Fatalf("CreateRevision(second) error = %v", err)
	}

	latest, err := store.LatestRevision(ctx, st.DB(), post.ID)
	if err != nil {
		t.Fatalf("LatestRevision() error = %v", err)
	}
	if latest.Title != "second" || latest.Excerpt != "e2" || latest.ContentHTML != "<p>2</p>" {
		t.Fatalf("LatestRevision() = %+v, want the 'second' snapshot", latest)
	}
	if latest.ContentJSON != `{"v":2}` {
		t.Fatalf("revision content_json = %q, want the supplied JSON", latest.ContentJSON)
	}
}

// TestPruneRevisionsKeeps50Newest inserts 55 revisions in a loop and, after
// pruning to 50, asserts exactly the newest 50 (revisions 6..55) remain.
func TestPruneRevisionsKeeps50Newest(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	author := createTestUser(t, st)
	post, err := store.CreatePost(ctx, st.DB(), store.NewPost{Title: "P", Slug: "p", Status: store.PostStatusDraft, Author: author.ID})
	if err != nil {
		t.Fatalf("CreatePost() error = %v", err)
	}

	const total = 55
	const keep = 50
	for i := 1; i <= total; i++ {
		if _, err := store.CreateRevision(ctx, st.DB(), store.NewRevision{
			PostID: post.ID, Title: fmt.Sprintf("rev-%d", i), ContentHTML: fmt.Sprintf("<p>%d</p>", i), EditorID: author.ID,
		}); err != nil {
			t.Fatalf("CreateRevision(%d) error = %v", i, err)
		}
		if _, err := store.PruneRevisions(ctx, st.DB(), post.ID, keep); err != nil {
			t.Fatalf("PruneRevisions() error = %v", err)
		}
	}

	count, err := store.CountRevisions(ctx, st.DB(), post.ID)
	if err != nil {
		t.Fatalf("CountRevisions() error = %v", err)
	}
	if count != keep {
		t.Fatalf("CountRevisions() = %d, want %d", count, keep)
	}

	latest, err := store.LatestRevision(ctx, st.DB(), post.ID)
	if err != nil {
		t.Fatalf("LatestRevision() error = %v", err)
	}
	if latest.Title != fmt.Sprintf("rev-%d", total) {
		t.Fatalf("LatestRevision().Title = %q, want rev-%d", latest.Title, total)
	}

	// The surviving titles are exactly rev-6 .. rev-55, in insertion order
	// (id ascending, since revisions created in the same second tie on
	// created_at and the monotonic UUIDv7 id breaks the tie).
	rows, err := st.DB().QueryContext(ctx,
		`SELECT title FROM post_revisions WHERE post_id = ? ORDER BY created_at ASC, id ASC`, post.ID)
	if err != nil {
		t.Fatalf("query revisions: %v", err)
	}
	defer rows.Close()

	var titles []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatalf("scan title: %v", err)
		}
		titles = append(titles, title)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate revisions: %v", err)
	}

	for i, title := range titles {
		want := fmt.Sprintf("rev-%d", i+6)
		if title != want {
			t.Fatalf("surviving revision[%d] = %q, want %q", i, title, want)
		}
	}
}
