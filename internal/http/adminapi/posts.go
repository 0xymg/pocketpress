package adminapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/http/middleware"
	"github.com/pocketpress/pocketpress/internal/store"
)

// Pagination bounds for the post list (frozen contract).
const (
	defaultPerPage = 20
	maxPerPage     = 100
)

// termRefDTO is the compact {id,name,slug} shape used for a post's
// categories and tags.
type termRefDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// mediaRefDTO is the compact media shape embedded as a post's cover_image /
// og_image: just enough to render an <img> without a second round trip to
// GET /media/{id}. A nil *mediaRefDTO serializes as JSON null.
type mediaRefDTO struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Alt    string `json:"alt"`
	Width  *int   `json:"width"`
	Height *int   `json:"height"`
}

// toMediaRefDTO builds the embedded post cover_image/og_image shape from a
// hydrated content.Post's *store.Media, passing nil through as nil (JSON
// null) for an unset cover/OG image.
func toMediaRefDTO(m *store.Media) *mediaRefDTO {
	if m == nil {
		return nil
	}
	return &mediaRefDTO{
		ID:     m.ID,
		URL:    mediaFileURL(m.ID, m.OriginalName),
		Alt:    m.Alt,
		Width:  m.Width,
		Height: m.Height,
	}
}

// postSummaryDTO is the frozen POST_SUMMARY shape (list rows and the base of
// the full post).
type postSummaryDTO struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Slug         string       `json:"slug"`
	Status       string       `json:"status"`
	Excerpt      string       `json:"excerpt"`
	Featured     bool         `json:"featured"`
	Author       authorDTO    `json:"author"`
	Categories   []termRefDTO `json:"categories"`
	Tags         []termRefDTO `json:"tags"`
	CoverImage   *mediaRefDTO `json:"cover_image"`
	OGImage      *mediaRefDTO `json:"og_image"`
	ScheduledFor *time.Time   `json:"scheduled_for"`
	PublishedAt  *time.Time   `json:"published_at"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// postFullDTO is the frozen POST_FULL shape: POST_SUMMARY plus the body and
// SEO fields. content_json is the serialized editor state and content_html is
// the server-sanitized HTML; the derived plain-text body (content_text) stays
// internal and is never serialized.
type postFullDTO struct {
	postSummaryDTO
	ContentJSON    string `json:"content_json"`
	ContentHTML    string `json:"content_html"`
	SEOTitle       string `json:"seo_title"`
	SEODescription string `json:"seo_description"`
	CanonicalURL   string `json:"canonical_url"`
}

func toPostSummaryDTO(p content.Post) postSummaryDTO {
	return postSummaryDTO{
		ID:           p.Row.ID,
		Title:        p.Row.Title,
		Slug:         p.Row.Slug,
		Status:       p.Row.Status,
		Excerpt:      p.Row.Excerpt,
		Featured:     p.Row.Featured,
		Author:       authorDTO{ID: p.Author.ID, Name: p.Author.Name, Slug: p.Author.Slug},
		Categories:   categoryRefs(p.Categories),
		Tags:         tagRefs(p.Tags),
		CoverImage:   toMediaRefDTO(p.CoverImage),
		OGImage:      toMediaRefDTO(p.OGImage),
		ScheduledFor: p.Row.ScheduledFor,
		PublishedAt:  p.Row.PublishedAt,
		CreatedAt:    p.Row.CreatedAt,
		UpdatedAt:    p.Row.UpdatedAt,
	}
}

func toPostFullDTO(p content.Post) postFullDTO {
	return postFullDTO{
		postSummaryDTO: toPostSummaryDTO(p),
		ContentJSON:    p.Row.ContentJSON,
		ContentHTML:    p.Row.ContentHTML,
		SEOTitle:       p.Row.SEOTitle,
		SEODescription: p.Row.SEODescription,
		CanonicalURL:   p.Row.CanonicalURL,
	}
}

func categoryRefs(cats []store.Category) []termRefDTO {
	refs := make([]termRefDTO, 0, len(cats))
	for _, c := range cats {
		refs = append(refs, termRefDTO{ID: c.ID, Name: c.Name, Slug: c.Slug})
	}
	return refs
}

func tagRefs(tags []store.Tag) []termRefDTO {
	refs := make([]termRefDTO, 0, len(tags))
	for _, t := range tags {
		refs = append(refs, termRefDTO{ID: t.ID, Name: t.Name, Slug: t.Slug})
	}
	return refs
}

// writePostFull writes {"post": POST_FULL} with the given status.
func (h *Handler) writePostFull(w http.ResponseWriter, status int, p content.Post) {
	middleware.WriteJSON(w, status, map[string]postFullDTO{"post": toPostFullDTO(p)})
}

// listPosts (GET /posts) returns a filtered, sorted, paginated page of post
// summaries plus pagination metadata.
func (h *Handler) listPosts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page := parsePositiveInt(q.Get("page"), 1)
	perPage := parsePositiveInt(q.Get("per_page"), defaultPerPage)
	if perPage > maxPerPage {
		perPage = maxPerPage
	}

	// sort allowlist: -updated_at (default, newest first) | updated_at |
	// title | -title. Unknown values fall back to the default (-updated_at).
	sortField, sortDesc := parsePostSort(q.Get("sort"))

	result, err := h.content.ListPosts(r.Context(), content.PostListQuery{
		Page:       page,
		PerPage:    perPage,
		Status:     q.Get("status"),
		CategoryID: q.Get("category"),
		Search:     q.Get("search"),
		Sort:       sortField,
		SortDesc:   sortDesc,
	})
	if err != nil {
		h.serverError(w, err)
		return
	}

	summaries := make([]postSummaryDTO, 0, len(result.Posts))
	for _, p := range result.Posts {
		summaries = append(summaries, toPostSummaryDTO(p))
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]any{
		"posts": summaries,
		"meta": map[string]int{
			"page":        result.Page,
			"per_page":    result.PerPage,
			"total_items": result.TotalItems,
			"total_pages": result.TotalPages,
		},
	})
}

// parsePostSort maps the admin list `sort` query param to a store sort field
// and direction. The allowlist is: "" / "-updated_at" (default, newest first),
// "updated_at", "title", "-title". Unknown values fall back to the default.
func parsePostSort(sort string) (store.PostSortField, bool) {
	switch sort {
	case "updated_at":
		return store.PostSortUpdatedAt, false
	case "title":
		return store.PostSortTitle, false
	case "-title":
		return store.PostSortTitle, true
	default: // "", "-updated_at", or anything unrecognized
		return store.PostSortUpdatedAt, true
	}
}

// createPost (POST /posts) creates a post (default draft) authored by the
// current user unless another author is given.
func (h *Handler) createPost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title          string   `json:"title"`
		Slug           *string  `json:"slug"`
		Excerpt        string   `json:"excerpt"`
		ContentJSON    string   `json:"content_json"`
		ContentHTML    string   `json:"content_html"`
		CoverImage     string   `json:"cover_image"`
		Status         string   `json:"status"`
		Author         string   `json:"author"`
		Categories     []string `json:"categories"`
		Tags           []string `json:"tags"`
		Featured       bool     `json:"featured"`
		SEOTitle       string   `json:"seo_title"`
		SEODescription string   `json:"seo_description"`
		CanonicalURL   string   `json:"canonical_url"`
		OGImage        string   `json:"og_image"`
	}
	if !h.decodePost(w, r, &req) {
		return
	}

	user, _ := middleware.UserFromContext(r.Context())
	p, err := h.content.CreatePost(r.Context(), content.PostCreateInput{
		Title:          req.Title,
		Slug:           req.Slug,
		Excerpt:        req.Excerpt,
		ContentJSON:    req.ContentJSON,
		ContentHTML:    req.ContentHTML,
		CoverImage:     req.CoverImage,
		Status:         req.Status,
		AuthorID:       req.Author,
		CategoryIDs:    req.Categories,
		TagIDs:         req.Tags,
		Featured:       req.Featured,
		SEOTitle:       req.SEOTitle,
		SEODescription: req.SEODescription,
		CanonicalURL:   req.CanonicalURL,
		OGImage:        req.OGImage,
		CurrentUserID:  user.ID,
	})
	if err != nil {
		h.handleContentError(w, err, "Post not found.")
		return
	}
	h.writePostFull(w, http.StatusCreated, p)
}

// getPost (GET /posts/{id}) returns a single full post.
func (h *Handler) getPost(w http.ResponseWriter, r *http.Request) {
	p, err := h.content.GetPost(r.Context(), r.PathValue("id"))
	if err != nil {
		h.handleContentError(w, err, "Post not found.")
		return
	}
	h.writePostFull(w, http.StatusOK, p)
}

// updatePost (PATCH /posts/{id}) applies a partial update (running revision
// logic).
func (h *Handler) updatePost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title          *string   `json:"title"`
		Slug           *string   `json:"slug"`
		Excerpt        *string   `json:"excerpt"`
		ContentJSON    *string   `json:"content_json"`
		ContentHTML    *string   `json:"content_html"`
		CoverImage     *string   `json:"cover_image"`
		Status         *string   `json:"status"`
		Author         *string   `json:"author"`
		Categories     *[]string `json:"categories"`
		Tags           *[]string `json:"tags"`
		Featured       *bool     `json:"featured"`
		SEOTitle       *string   `json:"seo_title"`
		SEODescription *string   `json:"seo_description"`
		CanonicalURL   *string   `json:"canonical_url"`
		OGImage        *string   `json:"og_image"`
	}
	if !h.decodePost(w, r, &req) {
		return
	}

	user, _ := middleware.UserFromContext(r.Context())
	p, err := h.content.UpdatePost(r.Context(), r.PathValue("id"), content.PostUpdateInput{
		Title:          req.Title,
		Slug:           req.Slug,
		Excerpt:        req.Excerpt,
		ContentJSON:    req.ContentJSON,
		ContentHTML:    req.ContentHTML,
		CoverImage:     req.CoverImage,
		Status:         req.Status,
		AuthorID:       req.Author,
		CategoryIDs:    req.Categories,
		TagIDs:         req.Tags,
		Featured:       req.Featured,
		SEOTitle:       req.SEOTitle,
		SEODescription: req.SEODescription,
		CanonicalURL:   req.CanonicalURL,
		OGImage:        req.OGImage,
		CurrentUserID:  user.ID,
	})
	if err != nil {
		h.handleContentError(w, err, "Post not found.")
		return
	}
	h.writePostFull(w, http.StatusOK, p)
}

// deletePost (DELETE /posts/{id}) removes a post (revisions and joins
// cascade).
func (h *Handler) deletePost(w http.ResponseWriter, r *http.Request) {
	if err := h.content.DeletePost(r.Context(), r.PathValue("id")); err != nil {
		h.handleContentError(w, err, "Post not found.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// duplicatePost (POST /posts/{id}/duplicate) copies a post into a fresh
// draft.
func (h *Handler) duplicatePost(w http.ResponseWriter, r *http.Request) {
	// The endpoint takes no body, but cap anything a client sends to the same
	// 5 MiB limit as the other post endpoints.
	r.Body = http.MaxBytesReader(w, r.Body, maxPostBodyBytes)
	user, _ := middleware.UserFromContext(r.Context())
	p, err := h.content.DuplicatePost(r.Context(), r.PathValue("id"), user.ID)
	if err != nil {
		h.handleContentError(w, err, "Post not found.")
		return
	}
	h.writePostFull(w, http.StatusCreated, p)
}

// parsePositiveInt parses s as a positive integer, returning def when s is
// empty, unparseable or less than 1.
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
