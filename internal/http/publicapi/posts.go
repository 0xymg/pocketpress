package publicapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/http/middleware"
	"github.com/pocketpress/pocketpress/internal/store"
)

// Pagination bounds for post listings (frozen contract, development plan
// section 6): page defaults to 1, per_page defaults to 20 and is capped at
// 100.
const (
	defaultPerPage = 20
	maxPerPage     = 100
)

// postDetailCacheControl is the post detail endpoint's own Cache-Control
// value — distinct from every other endpoint's listCacheControl, since the
// detail endpoint asks the client to always revalidate via ETag instead of
// relying on a max-age window.
const postDetailCacheControl = "public, max-age=0, must-revalidate"

// listPosts handles GET /api/v1/posts: a filtered, sorted, paginated page
// of publicly-visible posts.
func (h *Handler) listPosts(w http.ResponseWriter, r *http.Request) {
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
	authorID, authorOK, err := h.resolveAuthorFilter(r.Context(), q.Get("author"))
	if err != nil {
		h.serverError(w, err)
		return
	}

	// A category/tag/author filter naming an unknown value can never match
	// any post; short-circuit to an empty page rather than falling through
	// to an unfiltered (or wrongly-filtered) query.
	if !catOK || !tagOK || !authorOK {
		h.writePostList(w, r, nil, parsePage(q), parsePerPage(q), 0)
		return
	}

	query := content.PublicPostListQuery{
		Page:       parsePage(q),
		PerPage:    parsePerPage(q),
		CategoryID: catID,
		TagID:      tagID,
		Featured:   parseFeatured(q.Get("featured")),
		AuthorID:   authorID,
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

// getPost handles GET /api/v1/posts/{slug}: a single publicly-visible post,
// with ETag / If-None-Match support.
func (h *Handler) getPost(w http.ResponseWriter, r *http.Request) {
	p, err := h.content.GetPublicPostBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.postNotFound(w)
			return
		}
		h.serverError(w, err)
		return
	}

	etag := postETag(p.Row)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", resolveCacheControl(r.Context(), postDetailCacheControl))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if inm := r.Header.Get("If-None-Match"); inm != "" && inm == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]any{"data": toPostDTO(p)})
}

// writePostList maps posts to the public list envelope and writes it.
func (h *Handler) writePostList(w http.ResponseWriter, r *http.Request, posts []content.Post, page, perPage, totalItems int) {
	dtos := make([]postDTO, 0, len(posts))
	for _, p := range posts {
		dtos = append(dtos, toPostDTO(p))
	}
	totalPages := 0
	if perPage > 0 {
		totalPages = (totalItems + perPage - 1) / perPage
	}
	meta := metaDTO{Page: page, PerPage: perPage, TotalItems: totalItems, TotalPages: totalPages}
	h.writeListData(w, r, http.StatusOK, listCacheControl, dtos, meta)
}

// postETag builds the post detail endpoint's ETag: a quoted hex SHA-256 of
// the post's id concatenated with its updated_at timestamp. Any change to
// the post (which always bumps updated_at) changes the ETag; the id keeps
// two posts from ever colliding even if their updated_at happened to
// match.
func postETag(row store.Post) string {
	sum := sha256.Sum256([]byte(row.ID + row.UpdatedAt.UTC().Format(time.RFC3339)))
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

// resolveCategoryFilter resolves an optional "category" query parameter
// (a slug) to a category id. An empty slug means "no filter" (ok=true,
// id=""); an unknown slug means "no post can match" (ok=false).
func (h *Handler) resolveCategoryFilter(ctx context.Context, slug string) (id string, ok bool, err error) {
	if slug == "" {
		return "", true, nil
	}
	cat, err := h.content.GetCategoryBySlug(ctx, slug)
	if errors.Is(err, store.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return cat.ID, true, nil
}

// resolveTagFilter is resolveCategoryFilter's tag equivalent.
func (h *Handler) resolveTagFilter(ctx context.Context, slug string) (id string, ok bool, err error) {
	if slug == "" {
		return "", true, nil
	}
	tg, err := h.content.GetTagBySlug(ctx, slug)
	if errors.Is(err, store.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return tg.ID, true, nil
}

// resolveAuthorFilter resolves an optional "author" query parameter (user
// id or public author slug) to a user id.
func (h *Handler) resolveAuthorFilter(ctx context.Context, author string) (id string, ok bool, err error) {
	return h.content.ResolveAuthorFilter(ctx, author)
}

// parsePage parses the "page" query parameter, defaulting to 1 for an
// empty, unparseable or non-positive value.
func parsePage(q url.Values) int {
	return parsePositiveInt(q.Get("page"), 1)
}

// parsePerPage parses the "per_page" query parameter, defaulting to
// defaultPerPage and capping at maxPerPage.
func parsePerPage(q url.Values) int {
	n := parsePositiveInt(q.Get("per_page"), defaultPerPage)
	if n > maxPerPage {
		n = maxPerPage
	}
	return n
}

func parsePositiveInt(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return def
	}
	return n
}

// parseFeatured parses the "featured" query parameter ("true"/"false").
// An empty or unrecognized value means "no filter" (nil).
func parseFeatured(s string) *bool {
	if s == "" {
		return nil
	}
	b, err := strconv.ParseBool(s)
	if err != nil {
		return nil
	}
	return &b
}

// parseSort parses the "sort" query parameter: published_at, -published_at,
// title or -title. An empty or unrecognized value falls back to the
// documented default, -published_at (newest first).
func parseSort(s string) (store.PostSortField, bool) {
	switch s {
	case "published_at":
		return store.PostSortPublishedAt, false
	case "title":
		return store.PostSortTitle, false
	case "-title":
		return store.PostSortTitle, true
	default:
		return store.PostSortPublishedAt, true
	}
}
