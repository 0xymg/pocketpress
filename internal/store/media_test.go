package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/pocketpress/pocketpress/internal/store"
)

func newTestMedia(t *testing.T, st *store.Store, id, originalName string, uploadedBy string) store.Media {
	t.Helper()
	w, h := 4, 3
	m, err := store.CreateMedia(context.Background(), st.DB(), store.NewMedia{
		ID:           id,
		FileKey:      "2026/08/" + id + ".png",
		OriginalName: originalName,
		Mime:         "image/png",
		Size:         123,
		Width:        &w,
		Height:       &h,
		Storage:      "local",
		UploadedBy:   uploadedBy,
	})
	if err != nil {
		t.Fatalf("CreateMedia() error = %v", err)
	}
	return m
}

// TestMediaCRUD covers create, get, alt update and delete.
func TestMediaCRUD(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	user := createTestUser(t, st)

	m := newTestMedia(t, st, "media-1", "photo.png", user.ID)
	if m.Width == nil || *m.Width != 4 || m.Height == nil || *m.Height != 3 {
		t.Fatalf("CreateMedia() dimensions = %+v, want 4x3", m)
	}
	if m.Alt != "" {
		t.Fatalf("CreateMedia() Alt = %q, want empty default", m.Alt)
	}

	got, err := store.GetMediaByID(ctx, st.DB(), m.ID)
	if err != nil {
		t.Fatalf("GetMediaByID() error = %v", err)
	}
	if got.OriginalName != "photo.png" || got.FileKey != "2026/08/media-1.png" || got.UploadedBy != user.ID {
		t.Fatalf("GetMediaByID() = %+v", got)
	}

	if err := store.UpdateMediaAlt(ctx, st.DB(), m.ID, "A nice photo"); err != nil {
		t.Fatalf("UpdateMediaAlt() error = %v", err)
	}
	got, err = store.GetMediaByID(ctx, st.DB(), m.ID)
	if err != nil {
		t.Fatalf("GetMediaByID() (after alt update) error = %v", err)
	}
	if got.Alt != "A nice photo" {
		t.Fatalf("Alt after update = %q, want %q", got.Alt, "A nice photo")
	}

	if err := store.DeleteMedia(ctx, st.DB(), m.ID); err != nil {
		t.Fatalf("DeleteMedia() error = %v", err)
	}
	if _, err := store.GetMediaByID(ctx, st.DB(), m.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetMediaByID() after delete error = %v, want ErrNotFound", err)
	}
}

// TestMediaGetUpdateDeleteNotFound confirms Get/UpdateAlt/Delete all report
// ErrNotFound for an unknown id.
func TestMediaGetUpdateDeleteNotFound(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	if _, err := store.GetMediaByID(ctx, st.DB(), "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetMediaByID(unknown) error = %v, want ErrNotFound", err)
	}
	if err := store.UpdateMediaAlt(ctx, st.DB(), "nope", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("UpdateMediaAlt(unknown) error = %v, want ErrNotFound", err)
	}
	if err := store.DeleteMedia(ctx, st.DB(), "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DeleteMedia(unknown) error = %v, want ErrNotFound", err)
	}
}

// TestMediaUploadedByNulledOnUserDelete confirms media.uploaded_by is set to
// NULL ("" in Go) rather than blocking the user delete, per the
// ON DELETE SET NULL foreign key.
func TestMediaUploadedByNulledOnUserDelete(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	user := createTestUser(t, st)
	m := newTestMedia(t, st, "media-1", "photo.png", user.ID)

	if _, err := st.DB().ExecContext(ctx, `DELETE FROM users WHERE id = ?`, user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	got, err := store.GetMediaByID(ctx, st.DB(), m.ID)
	if err != nil {
		t.Fatalf("GetMediaByID() after uploader delete error = %v", err)
	}
	if got.UploadedBy != "" {
		t.Fatalf("UploadedBy after uploader delete = %q, want empty (NULL)", got.UploadedBy)
	}
}

// TestListMediaOrderingSearchAndPagination covers newest-first ordering, the
// case-insensitive original_name substring filter, and CountMedia matching
// ListMedia's filter.
func TestListMediaOrderingSearchAndPagination(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	user := createTestUser(t, st)

	newTestMedia(t, st, "media-1", "alpha.png", user.ID)
	newTestMedia(t, st, "media-2", "beta.png", user.ID)
	newTestMedia(t, st, "media-3", "gamma-report.png", user.ID)

	all, err := store.ListMedia(ctx, st.DB(), "", 10, 0)
	if err != nil {
		t.Fatalf("ListMedia() error = %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("ListMedia() length = %d, want 3", len(all))
	}
	// Newest first: media-3, media-2, media-1 (creation order == id order
	// here, and created_at is monotonic within the loop).
	if all[0].ID != "media-3" || all[2].ID != "media-1" {
		t.Fatalf("ListMedia() order = %v, want newest (media-3) first", []string{all[0].ID, all[1].ID, all[2].ID})
	}

	filtered, err := store.ListMedia(ctx, st.DB(), "REPORT", 10, 0)
	if err != nil {
		t.Fatalf("ListMedia(search) error = %v", err)
	}
	if len(filtered) != 1 || filtered[0].ID != "media-3" {
		t.Fatalf("ListMedia(search=REPORT) = %+v, want just media-3", filtered)
	}

	count, err := store.CountMedia(ctx, st.DB(), "REPORT")
	if err != nil {
		t.Fatalf("CountMedia(search) error = %v", err)
	}
	if count != 1 {
		t.Fatalf("CountMedia(search=REPORT) = %d, want 1", count)
	}

	page1, err := store.ListMedia(ctx, st.DB(), "", 2, 0)
	if err != nil {
		t.Fatalf("ListMedia(limit 2) error = %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("ListMedia(limit 2, offset 0) length = %d, want 2", len(page1))
	}
	page2, err := store.ListMedia(ctx, st.DB(), "", 2, 2)
	if err != nil {
		t.Fatalf("ListMedia(limit 2, offset 2) error = %v", err)
	}
	if len(page2) != 1 {
		t.Fatalf("ListMedia(limit 2, offset 2) length = %d, want 1", len(page2))
	}
}

// TestMediaInUse covers the delete guard's query: false when unreferenced,
// true when referenced by either cover_image or og_image, false again once
// the post is updated to clear the reference.
func TestMediaInUse(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	user := createTestUser(t, st)
	cover := newTestMedia(t, st, "media-cover", "cover.png", user.ID)
	og := newTestMedia(t, st, "media-og", "og.png", user.ID)
	unused := newTestMedia(t, st, "media-unused", "unused.png", user.ID)

	inUse, err := store.MediaInUse(ctx, st.DB(), unused.ID)
	if err != nil {
		t.Fatalf("MediaInUse(unused) error = %v", err)
	}
	if inUse {
		t.Fatal("MediaInUse(unused) = true, want false")
	}

	post, err := store.CreatePost(ctx, st.DB(), store.NewPost{
		Title:      "Has media",
		Slug:       "has-media",
		Status:     store.PostStatusDraft,
		Author:     user.ID,
		CoverImage: cover.ID,
		OGImage:    og.ID,
	})
	if err != nil {
		t.Fatalf("CreatePost() error = %v", err)
	}

	for _, m := range []store.Media{cover, og} {
		inUse, err := store.MediaInUse(ctx, st.DB(), m.ID)
		if err != nil {
			t.Fatalf("MediaInUse(%s) error = %v", m.OriginalName, err)
		}
		if !inUse {
			t.Fatalf("MediaInUse(%s) = false, want true (referenced by post)", m.OriginalName)
		}
	}

	if err := store.UpdatePost(ctx, st.DB(), post.ID, store.PostUpdate{
		Title:  post.Title,
		Slug:   post.Slug,
		Status: store.PostStatusDraft,
		Author: user.ID,
		// CoverImage/OGImage left "": cleared.
	}); err != nil {
		t.Fatalf("UpdatePost() (clear refs) error = %v", err)
	}

	for _, m := range []store.Media{cover, og} {
		inUse, err := store.MediaInUse(ctx, st.DB(), m.ID)
		if err != nil {
			t.Fatalf("MediaInUse(%s) (after clear) error = %v", m.OriginalName, err)
		}
		if inUse {
			t.Fatalf("MediaInUse(%s) (after clear) = true, want false", m.OriginalName)
		}
	}
}

// TestPostCoverAndOGImagePersist confirms CreatePost/UpdatePost round-trip
// the cover_image/og_image columns (added for Phase 4 alongside the media
// table).
func TestPostCoverAndOGImagePersist(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	user := createTestUser(t, st)
	cover := newTestMedia(t, st, "media-cover", "cover.png", user.ID)

	post, err := store.CreatePost(ctx, st.DB(), store.NewPost{
		Title:      "With cover",
		Slug:       "with-cover",
		Status:     store.PostStatusDraft,
		Author:     user.ID,
		CoverImage: cover.ID,
	})
	if err != nil {
		t.Fatalf("CreatePost() error = %v", err)
	}
	if post.CoverImage != cover.ID {
		t.Fatalf("CreatePost() CoverImage = %q, want %q", post.CoverImage, cover.ID)
	}

	got, err := store.GetPostByID(ctx, st.DB(), post.ID)
	if err != nil {
		t.Fatalf("GetPostByID() error = %v", err)
	}
	if got.CoverImage != cover.ID || got.OGImage != "" {
		t.Fatalf("GetPostByID() cover/og = %q/%q, want %q/\"\"", got.CoverImage, got.OGImage, cover.ID)
	}

	og := newTestMedia(t, st, "media-og", "og.png", user.ID)
	if err := store.UpdatePost(ctx, st.DB(), post.ID, store.PostUpdate{
		Title:      post.Title,
		Slug:       post.Slug,
		Status:     store.PostStatusDraft,
		Author:     user.ID,
		CoverImage: "", // clear
		OGImage:    og.ID,
	}); err != nil {
		t.Fatalf("UpdatePost() error = %v", err)
	}

	got, err = store.GetPostByID(ctx, st.DB(), post.ID)
	if err != nil {
		t.Fatalf("GetPostByID() (after update) error = %v", err)
	}
	if got.CoverImage != "" {
		t.Fatalf("CoverImage after clearing update = %q, want empty", got.CoverImage)
	}
	if got.OGImage != og.ID {
		t.Fatalf("OGImage after update = %q, want %q", got.OGImage, og.ID)
	}
}
