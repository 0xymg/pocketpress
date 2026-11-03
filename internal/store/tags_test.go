package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/pocketpress/pocketpress/internal/store"
)

// TestTagsCRUD exercises the tags repository end to end.
func TestTagsCRUD(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	if _, err := store.CreateTag(ctx, st.DB(), store.NewTag{Name: "Yaks", Slug: "yaks"}); err != nil {
		t.Fatalf("CreateTag(Yaks) error = %v", err)
	}
	golang, err := store.CreateTag(ctx, st.DB(), store.NewTag{Name: "Go", Slug: "go"})
	if err != nil {
		t.Fatalf("CreateTag(Go) error = %v", err)
	}

	got, err := store.GetTagByID(ctx, st.DB(), golang.ID)
	if err != nil {
		t.Fatalf("GetTagByID() error = %v", err)
	}
	if got.Name != "Go" || got.Slug != "go" {
		t.Fatalf("GetTagByID() = %+v, want Go/go", got)
	}

	list, err := store.ListTags(ctx, st.DB())
	if err != nil {
		t.Fatalf("ListTags() error = %v", err)
	}
	if len(list) != 2 || list[0].Name != "Go" || list[1].Name != "Yaks" {
		t.Fatalf("ListTags() ordering wrong: %+v", list)
	}

	if err := store.UpdateTag(ctx, st.DB(), golang.ID, "Golang", "golang"); err != nil {
		t.Fatalf("UpdateTag() error = %v", err)
	}
	updated, err := store.GetTagByID(ctx, st.DB(), golang.ID)
	if err != nil {
		t.Fatalf("GetTagByID() after update error = %v", err)
	}
	if updated.Name != "Golang" || updated.Slug != "golang" {
		t.Fatalf("after update = %+v, want Golang/golang", updated)
	}

	if err := store.DeleteTag(ctx, st.DB(), golang.ID); err != nil {
		t.Fatalf("DeleteTag() error = %v", err)
	}
	if _, err := store.GetTagByID(ctx, st.DB(), golang.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetTagByID() after delete error = %v, want ErrNotFound", err)
	}
}

// TestTagSlugExists mirrors the category exclude-self behavior.
func TestTagSlugExists(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	tg, err := store.CreateTag(ctx, st.DB(), store.NewTag{Name: "Go", Slug: "go"})
	if err != nil {
		t.Fatalf("CreateTag() error = %v", err)
	}

	exists, err := store.TagSlugExists(ctx, st.DB(), "go", "")
	if err != nil || !exists {
		t.Fatalf("TagSlugExists(go, \"\") = %v, %v; want true, nil", exists, err)
	}
	exists, err = store.TagSlugExists(ctx, st.DB(), "go", tg.ID)
	if err != nil || exists {
		t.Fatalf("TagSlugExists(go, self) = %v, %v; want false, nil", exists, err)
	}
}

// TestGetTagBySlug confirms slug lookup and its ErrNotFound case, used by
// the public API's tag detail and tag-scoped post-list endpoints.
func TestGetTagBySlug(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	tg, err := store.CreateTag(ctx, st.DB(), store.NewTag{Name: "Go", Slug: "go"})
	if err != nil {
		t.Fatalf("CreateTag() error = %v", err)
	}

	got, err := store.GetTagBySlug(ctx, st.DB(), "go")
	if err != nil {
		t.Fatalf("GetTagBySlug() error = %v", err)
	}
	if got.ID != tg.ID {
		t.Fatalf("GetTagBySlug() id = %q, want %q", got.ID, tg.ID)
	}

	if _, err := store.GetTagBySlug(ctx, st.DB(), "no-such-slug"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetTagBySlug(unknown) error = %v, want ErrNotFound", err)
	}
}

// TestTagNotFound confirms the update/delete ErrNotFound behavior.
func TestTagNotFound(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	if err := store.UpdateTag(ctx, st.DB(), "missing", "X", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("UpdateTag(missing) error = %v, want ErrNotFound", err)
	}
	if err := store.DeleteTag(ctx, st.DB(), "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DeleteTag(missing) error = %v, want ErrNotFound", err)
	}
}
