package publicapi

import (
	"errors"
	"net/http"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/store"
)

// listTags handles GET /api/v1/tags.
func (h *Handler) listTags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.content.ListTags(r.Context())
	if err != nil {
		h.serverError(w, err)
		return
	}
	dtos := make([]tagDTO, 0, len(tags))
	for _, t := range tags {
		dtos = append(dtos, toTagDTO(t))
	}
	h.writeData(w, r, http.StatusOK, listCacheControl, dtos)
}

// getTag handles GET /api/v1/tags/{slug}.
func (h *Handler) getTag(w http.ResponseWriter, r *http.Request) {
	tg, err := h.content.GetTagBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.tagNotFound(w)
			return
		}
		h.serverError(w, err)
		return
	}
	h.writeData(w, r, http.StatusOK, listCacheControl, toTagDTO(tg))
}

// tagPosts handles GET /api/v1/tags/{slug}/posts: the same
// filtered/sorted/paginated post list shape as GET /api/v1/posts, scoped
// to the named tag. Any "tag" query parameter is ignored — the path slug
// is authoritative.
func (h *Handler) tagPosts(w http.ResponseWriter, r *http.Request) {
	tg, err := h.content.GetTagBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.tagNotFound(w)
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
	authorID, authorOK, err := h.resolveAuthorFilter(r.Context(), q.Get("author"))
	if err != nil {
		h.serverError(w, err)
		return
	}
	localeFilter, localeOK := parseLocaleFilter(q.Get("locale"))
	if !catOK || !authorOK || !localeOK {
		h.writePostList(w, r, nil, parsePage(q), parsePerPage(q), 0)
		return
	}

	query := content.PublicPostListQuery{
		Page:       parsePage(q),
		PerPage:    parsePerPage(q),
		CategoryID: catID,
		TagID:      tg.ID,
		Featured:   parseFeatured(q.Get("featured")),
		AuthorID:   authorID,
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
