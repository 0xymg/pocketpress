package publicapi

import (
	"errors"
	"net/http"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/store"
)

// listAuthors handles GET /api/v1/authors.
func (h *Handler) listAuthors(w http.ResponseWriter, r *http.Request) {
	authors, err := h.content.ListAuthors(r.Context())
	if err != nil {
		h.serverError(w, err)
		return
	}
	dtos := make([]authorDTO, 0, len(authors))
	for _, a := range authors {
		dtos = append(dtos, toAuthorDTO(a))
	}
	h.writeData(w, r, http.StatusOK, listCacheControl, dtos)
}

// getAuthor handles GET /api/v1/authors/{slug}.
func (h *Handler) getAuthor(w http.ResponseWriter, r *http.Request) {
	author, err := h.content.GetAuthorBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.authorNotFound(w)
			return
		}
		h.serverError(w, err)
		return
	}
	h.writeData(w, r, http.StatusOK, listCacheControl, toAuthorDTO(author))
}

// authorPosts handles GET /api/v1/authors/{slug}/posts: the same
// filtered/sorted/paginated post list shape as GET /api/v1/posts, scoped
// to the named author. Any "author" query parameter is ignored — the path
// slug is authoritative.
func (h *Handler) authorPosts(w http.ResponseWriter, r *http.Request) {
	author, err := h.content.GetAuthorBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.authorNotFound(w)
			return
		}
		h.serverError(w, err)
		return
	}

	q := r.URL.Query()
	catID, catOK, err := h.resolveCategoryFilter(r.Context(), q.Get("category"))
	if err != nil {
		h.serverError(w, err)
		return
	}
	tagID, tagOK, err := h.resolveTagFilter(r.Context(), q.Get("tag"))
	if err != nil {
		h.serverError(w, err)
		return
	}
	localeFilter, localeOK := parseLocaleFilter(q.Get("locale"))
	if !catOK || !tagOK || !localeOK {
		h.writePostList(w, r, nil, parsePage(q), parsePerPage(q), 0)
		return
	}

	query := content.PublicPostListQuery{
		Page:       parsePage(q),
		PerPage:    parsePerPage(q),
		CategoryID: catID,
		TagID:      tagID,
		Featured:   parseFeatured(q.Get("featured")),
		AuthorID:   author.ID,
		Locale:     localeFilter,
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
