package publicapi

import (
	"errors"
	"net/http"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/store"
)

// listCategories handles GET /api/v1/categories.
func (h *Handler) listCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := h.content.ListCategories(r.Context())
	if err != nil {
		h.serverError(w, err)
		return
	}
	dtos := make([]categoryDTO, 0, len(cats))
	for _, c := range cats {
		dtos = append(dtos, toCategoryDTO(c))
	}
	h.writeData(w, r, http.StatusOK, listCacheControl, dtos)
}

// getCategory handles GET /api/v1/categories/{slug}.
func (h *Handler) getCategory(w http.ResponseWriter, r *http.Request) {
	cat, err := h.content.GetCategoryBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.categoryNotFound(w)
			return
		}
		h.serverError(w, err)
		return
	}
	h.writeData(w, r, http.StatusOK, listCacheControl, toCategoryDTO(cat))
}

// categoryPosts handles GET /api/v1/categories/{slug}/posts: the same
// filtered/sorted/paginated post list shape as GET /api/v1/posts, scoped
// to the named category. Any "category" query parameter is ignored — the
// path slug is authoritative.
func (h *Handler) categoryPosts(w http.ResponseWriter, r *http.Request) {
	cat, err := h.content.GetCategoryBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.categoryNotFound(w)
			return
		}
		h.serverError(w, err)
		return
	}

	q := r.URL.Query()
	tagID, tagOK, err := h.resolveTagFilter(r.Context(), q.Get("tag"))
	if err != nil {
		h.serverError(w, err)
		return
	}
	if !tagOK {
		h.writePostList(w, r, nil, parsePage(q), parsePerPage(q), 0)
		return
	}

	query := content.PublicPostListQuery{
		Page:       parsePage(q),
		PerPage:    parsePerPage(q),
		CategoryID: cat.ID,
		TagID:      tagID,
		Featured:   parseFeatured(q.Get("featured")),
		AuthorID:   q.Get("author"),
		Search:     q.Get("q"),
	}
	query.Sort, query.SortDesc = parseSort(q.Get("sort"))

	result, err := h.content.ListPublicPosts(r.Context(), query)
	if err != nil {
		h.serverError(w, err)
		return
	}
	h.writePostList(w, r, result.Posts, result.Page, result.PerPage, result.TotalItems)
}
