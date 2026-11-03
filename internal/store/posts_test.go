package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

// setPublishedAt sets a post's published_at column directly via SQL. There
// is no store-level "publish" function yet (that arrives in Phase 6), so
// tests that need a post with a known published_at (to exercise the public
// API's visibility filter, PostFilter.PublishedBefore) set it this way,
// matching the development plan's Phase 5 instruction to seed publish
// state directly through the store or SQL.
func setPublishedAt(t *testing.T, st *store.Store, postID string, at time.Time) {
	t.Helper()
	_, err := st.DB().Exec(`UPDATE posts SET published_at = ? WHERE id = ?`, at.UTC().Format(time.RFC3339), postID)
	if err != nil {
		t.Fatalf("set published_at: %v", err)
	}
}

// TestPostsCRUDAndJoins covers create, get, update, the category/tag join
// helpers and cascade-on-delete.
func TestPostsCRUDAndJoins(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	author := createTestUser(t, st)

	cat, err := store.CreateCategory(ctx, st.DB(), store.NewCategory{Name: "News", Slug: "news"})
	if err != nil {
		t.Fatalf("CreateCategory() error = %v", err)
	}
	tag, err := store.CreateTag(ctx, st.DB(), store.NewTag{Name: "Go", Slug: "go"})
	if err != nil {
		t.Fatalf("CreateTag() error = %v", err)
	}

	post, err := store.CreatePost(ctx, st.DB(), store.NewPost{
		Title:       "Hello",
		Slug:        "hello",
		Excerpt:     "excerpt",
		ContentJSON: `{"root":1}`,
		ContentHTML: "<p>body</p>",
		ContentText: "body",
		Status:      store.PostStatusDraft,
		Author:      author.ID,
	})
	if err != nil {
		t.Fatalf("CreatePost() error = %v", err)
	}
	if post.ContentJSON != `{"root":1}` {
		t.Fatalf("CreatePost() content_json = %q, want the supplied JSON", post.ContentJSON)
	}

	got, err := store.GetPostByID(ctx, st.DB(), post.ID)
	if err != nil {
		t.Fatalf("GetPostByID() error = %v", err)
	}
	if got.Title != "Hello" || got.Excerpt != "excerpt" || got.Status != store.PostStatusDraft {
		t.Fatalf("GetPostByID() = %+v", got)
	}
	if got.ContentJSON != `{"root":1}` || got.ContentHTML != "<p>body</p>" || got.ContentText != "body" {
		t.Fatalf("GetPostByID() content = json:%q html:%q text:%q", got.ContentJSON, got.ContentHTML, got.ContentText)
	}
	if got.PublishedAt != nil || got.ScheduledFor != nil {
		t.Fatalf("new post should have nil published_at/scheduled_for, got %+v/%+v", got.PublishedAt, got.ScheduledFor)
	}

	// Associate a category and a tag; the get-helpers should return them.
	if err := store.SetPostCategories(ctx, st.DB(), post.ID, []string{cat.ID, cat.ID}); err != nil {
		t.Fatalf("SetPostCategories() error = %v", err)
	}
	if err := store.SetPostTags(ctx, st.DB(), post.ID, []string{tag.ID}); err != nil {
		t.Fatalf("SetPostTags() error = %v", err)
	}
	cats, err := store.GetPostCategories(ctx, st.DB(), post.ID)
	if err != nil {
		t.Fatalf("GetPostCategories() error = %v", err)
	}
	if len(cats) != 1 || cats[0].ID != cat.ID {
		t.Fatalf("GetPostCategories() = %+v, want one (deduped) News", cats)
	}
	tags, err := store.GetPostTags(ctx, st.DB(), post.ID)
	if err != nil {
		t.Fatalf("GetPostTags() error = %v", err)
	}
	if len(tags) != 1 || tags[0].ID != tag.ID {
		t.Fatalf("GetPostTags() = %+v, want one Go", tags)
	}

	// Update, including clearing the category set.
	if err := store.UpdatePost(ctx, st.DB(), post.ID, store.PostUpdate{
		Title:       "Hello Edited",
		Slug:        "hello-edited",
		ContentJSON: `{"root":2}`,
		ContentHTML: "<p>new</p>",
		ContentText: "new",
		Status:      store.PostStatusArchived,
		Author:      author.ID,
	}); err != nil {
		t.Fatalf("UpdatePost() error = %v", err)
	}
	if err := store.SetPostCategories(ctx, st.DB(), post.ID, nil); err != nil {
		t.Fatalf("SetPostCategories(nil) error = %v", err)
	}
	cats, err = store.GetPostCategories(ctx, st.DB(), post.ID)
	if err != nil {
		t.Fatalf("GetPostCategories() after clear error = %v", err)
	}
	if len(cats) != 0 {
		t.Fatalf("GetPostCategories() after clear = %+v, want empty", cats)
	}
	edited, err := store.GetPostByID(ctx, st.DB(), post.ID)
	if err != nil {
		t.Fatalf("GetPostByID() after update error = %v", err)
	}
	if edited.Title != "Hello Edited" || edited.Status != store.PostStatusArchived {
		t.Fatalf("after update = %+v", edited)
	}
	if edited.ContentJSON != `{"root":2}` || edited.ContentHTML != "<p>new</p>" || edited.ContentText != "new" {
		t.Fatalf("after update content = json:%q html:%q text:%q", edited.ContentJSON, edited.ContentHTML, edited.ContentText)
	}

	// Deleting the post cascades its revisions and remaining tag joins.
	if _, err := store.CreateRevision(ctx, st.DB(), store.NewRevision{
		PostID: post.ID, Title: "Hello", ContentHTML: "<p>body</p>", EditorID: author.ID,
	}); err != nil {
		t.Fatalf("CreateRevision() error = %v", err)
	}
	if err := store.DeletePost(ctx, st.DB(), post.ID); err != nil {
		t.Fatalf("DeletePost() error = %v", err)
	}
	if _, err := store.GetPostByID(ctx, st.DB(), post.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetPostByID() after delete error = %v, want ErrNotFound", err)
	}
	if n, err := store.CountRevisions(ctx, st.DB(), post.ID); err != nil || n != 0 {
		t.Fatalf("CountRevisions() after post delete = %d, %v; want 0, nil", n, err)
	}
}

// TestDeleteCategoryKeepsPosts confirms deleting a category removes only the
// join rows, leaving the post intact (FK ON DELETE CASCADE on the join).
func TestDeleteCategoryKeepsPosts(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	author := createTestUser(t, st)

	cat, err := store.CreateCategory(ctx, st.DB(), store.NewCategory{Name: "News", Slug: "news"})
	if err != nil {
		t.Fatalf("CreateCategory() error = %v", err)
	}
	post, err := store.CreatePost(ctx, st.DB(), store.NewPost{
		Title: "Keep me", Slug: "keep-me", Status: store.PostStatusDraft, Author: author.ID,
	})
	if err != nil {
		t.Fatalf("CreatePost() error = %v", err)
	}
	if err := store.SetPostCategories(ctx, st.DB(), post.ID, []string{cat.ID}); err != nil {
		t.Fatalf("SetPostCategories() error = %v", err)
	}

	if err := store.DeleteCategory(ctx, st.DB(), cat.ID); err != nil {
		t.Fatalf("DeleteCategory() error = %v", err)
	}
	if _, err := store.GetPostByID(ctx, st.DB(), post.ID); err != nil {
		t.Fatalf("post should survive category delete, GetPostByID() error = %v", err)
	}
	cats, err := store.GetPostCategories(ctx, st.DB(), post.ID)
	if err != nil {
		t.Fatalf("GetPostCategories() error = %v", err)
	}
	if len(cats) != 0 {
		t.Fatalf("join rows should be gone, got %+v", cats)
	}
}

// TestListPostsFilters covers status, category and search filtering, the
// sort direction and the count used for pagination.
func TestListPostsFilters(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	author := createTestUser(t, st)

	cat, err := store.CreateCategory(ctx, st.DB(), store.NewCategory{Name: "News", Slug: "news"})
	if err != nil {
		t.Fatalf("CreateCategory() error = %v", err)
	}

	// draft "Alpha" in News, archived "Beta" with no category, draft "Gamma".
	alpha, _ := store.CreatePost(ctx, st.DB(), store.NewPost{Title: "Alpha", Slug: "alpha", Status: store.PostStatusDraft, Author: author.ID})
	if err := store.SetPostCategories(ctx, st.DB(), alpha.ID, []string{cat.ID}); err != nil {
		t.Fatalf("SetPostCategories() error = %v", err)
	}
	if _, err := store.CreatePost(ctx, st.DB(), store.NewPost{Title: "Beta", Slug: "beta", Status: store.PostStatusArchived, Author: author.ID}); err != nil {
		t.Fatalf("CreatePost(Beta) error = %v", err)
	}
	if _, err := store.CreatePost(ctx, st.DB(), store.NewPost{Title: "Gamma", Slug: "gamma", Status: store.PostStatusDraft, Author: author.ID}); err != nil {
		t.Fatalf("CreatePost(Gamma) error = %v", err)
	}

	assertCount := func(name string, f store.PostFilter, want int) {
		n, err := store.CountPosts(ctx, st.DB(), f)
		if err != nil {
			t.Fatalf("%s CountPosts() error = %v", name, err)
		}
		if n != want {
			t.Fatalf("%s CountPosts() = %d, want %d", name, n, want)
		}
		rows, err := store.ListPosts(ctx, st.DB(), f, store.PostSortUpdatedAt, true, 100, 0)
		if err != nil {
			t.Fatalf("%s ListPosts() error = %v", name, err)
		}
		if len(rows) != want {
			t.Fatalf("%s ListPosts() len = %d, want %d", name, len(rows), want)
		}
	}

	assertCount("all", store.PostFilter{}, 3)
	assertCount("status draft", store.PostFilter{Status: store.PostStatusDraft}, 2)
	assertCount("status archived", store.PostFilter{Status: store.PostStatusArchived}, 1)
	assertCount("category News", store.PostFilter{CategoryID: cat.ID}, 1)
	assertCount("search case-insensitive", store.PostFilter{Search: "eta"}, 1) // matches "Beta"
	assertCount("search miss", store.PostFilter{Search: "zzz"}, 0)

	// Pagination: page size 2 over 3 items returns 2 then 1.
	first, err := store.ListPosts(ctx, st.DB(), store.PostFilter{}, store.PostSortUpdatedAt, true, 2, 0)
	if err != nil || len(first) != 2 {
		t.Fatalf("ListPosts(limit 2, offset 0) len = %d, err = %v; want 2", len(first), err)
	}
	second, err := store.ListPosts(ctx, st.DB(), store.PostFilter{}, store.PostSortUpdatedAt, true, 2, 2)
	if err != nil || len(second) != 1 {
		t.Fatalf("ListPosts(limit 2, offset 2) len = %d, err = %v; want 1", len(second), err)
	}
}

// TestPostSlugExists mirrors the category/tag exclude-self behavior.
func TestPostSlugExists(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	author := createTestUser(t, st)

	p, err := store.CreatePost(ctx, st.DB(), store.NewPost{Title: "Hi", Slug: "hi", Status: store.PostStatusDraft, Author: author.ID})
	if err != nil {
		t.Fatalf("CreatePost() error = %v", err)
	}

	exists, err := store.PostSlugExists(ctx, st.DB(), "hi", "")
	if err != nil || !exists {
		t.Fatalf("PostSlugExists(hi, \"\") = %v, %v; want true, nil", exists, err)
	}
	exists, err = store.PostSlugExists(ctx, st.DB(), "hi", p.ID)
	if err != nil || exists {
		t.Fatalf("PostSlugExists(hi, self) = %v, %v; want false, nil", exists, err)
	}
}

// TestGetPostBySlug confirms slug lookup returns the matching row
// regardless of status, and ErrNotFound for an unknown slug — callers that
// must only surface public posts (the public API) apply that check
// themselves on top of this lookup.
func TestGetPostBySlug(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	author := createTestUser(t, st)

	p, err := store.CreatePost(ctx, st.DB(), store.NewPost{
		Title: "Hello", Slug: "hello", Status: store.PostStatusDraft, Author: author.ID,
	})
	if err != nil {
		t.Fatalf("CreatePost() error = %v", err)
	}

	got, err := store.GetPostBySlug(ctx, st.DB(), "hello")
	if err != nil {
		t.Fatalf("GetPostBySlug() error = %v", err)
	}
	if got.ID != p.ID || got.Status != store.PostStatusDraft {
		t.Fatalf("GetPostBySlug() = %+v, want id %q status draft", got, p.ID)
	}

	if _, err := store.GetPostBySlug(ctx, st.DB(), "no-such-slug"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetPostBySlug(unknown) error = %v, want ErrNotFound", err)
	}
}

// TestListPostsFeaturedTagAuthorFilters covers the public API's remaining
// PostFilter fields not already exercised by TestListPostsFilters:
// Featured, TagID and AuthorID.
func TestListPostsFeaturedTagAuthorFilters(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	authorA := createTestUser(t, st)
	authorB, err := store.CreateUser(ctx, st.DB(), store.NewUser{
		Email: "editor@example.com", PasswordHash: "hash", Name: "Editor", Role: store.RoleEditor,
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	tag, err := store.CreateTag(ctx, st.DB(), store.NewTag{Name: "Go", Slug: "go"})
	if err != nil {
		t.Fatalf("CreateTag() error = %v", err)
	}

	one, err := store.CreatePost(ctx, st.DB(), store.NewPost{
		Title: "One", Slug: "one", Status: store.PostStatusDraft, Author: authorA.ID, Featured: true,
	})
	if err != nil {
		t.Fatalf("CreatePost(One) error = %v", err)
	}
	if err := store.SetPostTags(ctx, st.DB(), one.ID, []string{tag.ID}); err != nil {
		t.Fatalf("SetPostTags() error = %v", err)
	}
	if _, err := store.CreatePost(ctx, st.DB(), store.NewPost{
		Title: "Two", Slug: "two", Status: store.PostStatusDraft, Author: authorB.ID, Featured: false,
	}); err != nil {
		t.Fatalf("CreatePost(Two) error = %v", err)
	}

	assertCount := func(name string, f store.PostFilter, want int) {
		t.Helper()
		n, err := store.CountPosts(ctx, st.DB(), f)
		if err != nil {
			t.Fatalf("%s CountPosts() error = %v", name, err)
		}
		if n != want {
			t.Fatalf("%s CountPosts() = %d, want %d", name, n, want)
		}
	}

	featuredTrue, featuredFalse := true, false
	assertCount("featured=true", store.PostFilter{Featured: &featuredTrue}, 1)
	assertCount("featured=false", store.PostFilter{Featured: &featuredFalse}, 1)
	assertCount("tag go", store.PostFilter{TagID: tag.ID}, 1)
	assertCount("author A", store.PostFilter{AuthorID: authorA.ID}, 1)
	assertCount("author B", store.PostFilter{AuthorID: authorB.ID}, 1)
	assertCount("author unknown", store.PostFilter{AuthorID: "no-such-author"}, 0)
}

// TestListPostsPublishedBeforeFilter covers PostFilter.PublishedBefore, the
// public API's visibility building block: only rows with a non-NULL
// published_at at or before the cutoff match; a NULL published_at (never
// published) or one in the future never do.
func TestListPostsPublishedBeforeFilter(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	author := createTestUser(t, st)

	now := time.Now().UTC()

	past, err := store.CreatePost(ctx, st.DB(), store.NewPost{Title: "Past", Slug: "past", Status: store.PostStatusPublished, Author: author.ID})
	if err != nil {
		t.Fatalf("CreatePost(Past) error = %v", err)
	}
	setPublishedAt(t, st, past.ID, now.Add(-time.Hour))

	future, err := store.CreatePost(ctx, st.DB(), store.NewPost{Title: "Future", Slug: "future", Status: store.PostStatusPublished, Author: author.ID})
	if err != nil {
		t.Fatalf("CreatePost(Future) error = %v", err)
	}
	setPublishedAt(t, st, future.ID, now.Add(time.Hour))

	if _, err := store.CreatePost(ctx, st.DB(), store.NewPost{Title: "Never", Slug: "never", Status: store.PostStatusDraft, Author: author.ID}); err != nil {
		t.Fatalf("CreatePost(Never) error = %v", err)
	}

	rows, err := store.ListPosts(ctx, st.DB(), store.PostFilter{PublishedBefore: &now}, store.PostSortUpdatedAt, true, 100, 0)
	if err != nil {
		t.Fatalf("ListPosts(PublishedBefore) error = %v", err)
	}
	if len(rows) != 1 || rows[0].ID != past.ID {
		t.Fatalf("ListPosts(PublishedBefore=now) = %+v, want only %q", rows, past.ID)
	}
}

// TestListPostsSortFields covers PostSortPublishedAt and PostSortTitle
// (PostSortUpdatedAt is already exercised throughout this file).
func TestListPostsSortFields(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	author := createTestUser(t, st)
	now := time.Now().UTC()

	// Titles and publish times are deliberately out of insertion order so a
	// sort actually reorders the rows.
	c, err := store.CreatePost(ctx, st.DB(), store.NewPost{Title: "Charlie", Slug: "charlie", Status: store.PostStatusPublished, Author: author.ID})
	if err != nil {
		t.Fatalf("CreatePost(Charlie) error = %v", err)
	}
	setPublishedAt(t, st, c.ID, now.Add(-3*time.Hour))

	a, err := store.CreatePost(ctx, st.DB(), store.NewPost{Title: "alpha", Slug: "alpha", Status: store.PostStatusPublished, Author: author.ID})
	if err != nil {
		t.Fatalf("CreatePost(alpha) error = %v", err)
	}
	setPublishedAt(t, st, a.ID, now.Add(-2*time.Hour))

	b, err := store.CreatePost(ctx, st.DB(), store.NewPost{Title: "Bravo", Slug: "bravo", Status: store.PostStatusPublished, Author: author.ID})
	if err != nil {
		t.Fatalf("CreatePost(Bravo) error = %v", err)
	}
	setPublishedAt(t, st, b.ID, now.Add(-1*time.Hour))

	// Title ascending, case-insensitive: alpha, Bravo, Charlie.
	rows, err := store.ListPosts(ctx, st.DB(), store.PostFilter{}, store.PostSortTitle, false, 100, 0)
	if err != nil {
		t.Fatalf("ListPosts(PostSortTitle asc) error = %v", err)
	}
	if got := titles(rows); !equalStrings(got, []string{"alpha", "Bravo", "Charlie"}) {
		t.Fatalf("ListPosts(PostSortTitle asc) titles = %v, want [alpha Bravo Charlie]", got)
	}

	// published_at descending: Bravo (latest), alpha, Charlie (earliest).
	rows, err = store.ListPosts(ctx, st.DB(), store.PostFilter{}, store.PostSortPublishedAt, true, 100, 0)
	if err != nil {
		t.Fatalf("ListPosts(PostSortPublishedAt desc) error = %v", err)
	}
	if got := titles(rows); !equalStrings(got, []string{"Bravo", "alpha", "Charlie"}) {
		t.Fatalf("ListPosts(PostSortPublishedAt desc) titles = %v, want [Bravo alpha Charlie]", got)
	}
}

func titles(posts []store.Post) []string {
	out := make([]string, len(posts))
	for i, p := range posts {
		out[i] = p.Title
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
