package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/pocketpress/pocketpress/internal/store"
)

// TestCategoriesCRUD exercises the categories repository end to end.
func TestCategoriesCRUD(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	// Insert out of alphabetical order to prove ListCategories sorts by name.
	if _, err := store.CreateCategory(ctx, st.DB(), store.NewCategory{Name: "Zeta", Slug: "zeta"}); err != nil {
		t.Fatalf("CreateCategory(Zeta) error = %v", err)
	}
	alpha, err := store.CreateCategory(ctx, st.DB(), store.NewCategory{
		Name: "Alpha", Slug: "alpha", Description: "first",
	})
	if err != nil {
		t.Fatalf("CreateCategory(Alpha) error = %v", err)
	}

	got, err := store.GetCategoryByID(ctx, st.DB(), alpha.ID)
	if err != nil {
		t.Fatalf("GetCategoryByID() error = %v", err)
	}
	if got.Name != "Alpha" || got.Slug != "alpha" || got.Description != "first" {
		t.Fatalf("GetCategoryByID() = %+v, want Alpha/alpha/first", got)
	}

	list, err := store.ListCategories(ctx, st.DB())
	if err != nil {
		t.Fatalf("ListCategories() error = %v", err)
	}
	if len(list) != 2 || list[0].Name != "Alpha" || list[1].Name != "Zeta" {
		t.Fatalf("ListCategories() = %v, want [Alpha, Zeta]", names(list))
	}

	if err := store.UpdateCategory(ctx, st.DB(), alpha.ID, "Alpha Renamed", "alpha-renamed", ""); err != nil {
		t.Fatalf("UpdateCategory() error = %v", err)
	}
	updated, err := store.GetCategoryByID(ctx, st.DB(), alpha.ID)
	if err != nil {
		t.Fatalf("GetCategoryByID() after update error = %v", err)
	}
	if updated.Name != "Alpha Renamed" || updated.Slug != "alpha-renamed" || updated.Description != "" {
		t.Fatalf("after update = %+v, want Alpha Renamed/alpha-renamed/empty", updated)
	}

	if err := store.DeleteCategory(ctx, st.DB(), alpha.ID); err != nil {
		t.Fatalf("DeleteCategory() error = %v", err)
	}
	if _, err := store.GetCategoryByID(ctx, st.DB(), alpha.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetCategoryByID() after delete error = %v, want ErrNotFound", err)
	}
}

// TestCategorySlugExists confirms the exclude-self behavior used to allow a
// category to keep its own slug on update.
func TestCategorySlugExists(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	c, err := store.CreateCategory(ctx, st.DB(), store.NewCategory{Name: "News", Slug: "news"})
	if err != nil {
		t.Fatalf("CreateCategory() error = %v", err)
	}

	// Taken by another (excludeID = "").
	exists, err := store.CategorySlugExists(ctx, st.DB(), "news", "")
	if err != nil || !exists {
		t.Fatalf("CategorySlugExists(news, \"\") = %v, %v; want true, nil", exists, err)
	}
	// Excluding the owning row, its own slug is free.
	exists, err = store.CategorySlugExists(ctx, st.DB(), "news", c.ID)
	if err != nil || exists {
		t.Fatalf("CategorySlugExists(news, self) = %v, %v; want false, nil", exists, err)
	}
	// Unknown slug is free.
	exists, err = store.CategorySlugExists(ctx, st.DB(), "sports", "")
	if err != nil || exists {
		t.Fatalf("CategorySlugExists(sports, \"\") = %v, %v; want false, nil", exists, err)
	}
}

// TestCategoryNotFound confirms the update/delete ErrNotFound behavior.
func TestCategoryNotFound(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	if err := store.UpdateCategory(ctx, st.DB(), "missing", "X", "x", ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("UpdateCategory(missing) error = %v, want ErrNotFound", err)
	}
	if err := store.DeleteCategory(ctx, st.DB(), "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DeleteCategory(missing) error = %v, want ErrNotFound", err)
	}
}

// TestGetCategoryBySlug confirms slug lookup and its ErrNotFound case, used
// by the public API's category detail and category-scoped post-list
// endpoints.
func TestGetCategoryBySlug(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	cat, err := store.CreateCategory(ctx, st.DB(), store.NewCategory{Name: "News", Slug: "news"})
	if err != nil {
		t.Fatalf("CreateCategory() error = %v", err)
	}

	got, err := store.GetCategoryBySlug(ctx, st.DB(), "news")
	if err != nil {
		t.Fatalf("GetCategoryBySlug() error = %v", err)
	}
	if got.ID != cat.ID {
		t.Fatalf("GetCategoryBySlug() id = %q, want %q", got.ID, cat.ID)
	}

	if _, err := store.GetCategoryBySlug(ctx, st.DB(), "no-such-slug"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetCategoryBySlug(unknown) error = %v, want ErrNotFound", err)
	}
}

func names(cats []store.Category) []string {
	out := make([]string, len(cats))
	for i, c := range cats {
		out[i] = c.Name
	}
	return out
}
