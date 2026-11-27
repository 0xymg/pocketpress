package adminapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/http/middleware"
	"github.com/pocketpress/pocketpress/internal/store"
)

// handleContentError maps errors from the content service to the frozen HTTP
// responses: a *content.ValidationError becomes the 400 validation_failed
// envelope (its field map used verbatim), store.ErrNotFound becomes a 404
// with notFoundMessage, and anything else becomes a generic 500.
func (h *Handler) handleContentError(w http.ResponseWriter, err error, notFoundMessage string) {
	var ve *content.ValidationError
	if errors.As(err, &ve) {
		h.validationError(w, ve.Fields)
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		middleware.WriteError(w, http.StatusNotFound, "not_found", notFoundMessage)
		return
	}
	h.serverError(w, err)
}

// categoryDTO is the public JSON shape of a category (frozen CAT contract).
type categoryDTO struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toCategoryDTO(c store.Category) categoryDTO {
	return categoryDTO{
		ID:          c.ID,
		Name:        c.Name,
		Slug:        c.Slug,
		Description: c.Description,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
}

// tagDTO is the public JSON shape of a tag (frozen TAG contract; no
// description).
type tagDTO struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toTagDTO(t store.Tag) tagDTO {
	return tagDTO{
		ID:        t.ID,
		Name:      t.Name,
		Slug:      t.Slug,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

// authorDTO is the admin author projection used by the authors endpoint
// and embedded in post summaries. It never carries email or role.
type authorDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// listAuthors (GET /authors) returns active users for the author selector.
// Available to any authenticated user.
func (h *Handler) listAuthors(w http.ResponseWriter, r *http.Request) {
	authors, err := h.content.ListAuthors(r.Context())
	if err != nil {
		h.serverError(w, err)
		return
	}
	dtos := make([]authorDTO, 0, len(authors))
	for _, a := range authors {
		dtos = append(dtos, authorDTO{ID: a.ID, Name: a.Name, Slug: a.Slug})
	}
	middleware.WriteJSON(w, http.StatusOK, map[string][]authorDTO{"authors": dtos})
}

// ---------------------------------------------------------------------------
// Categories
// ---------------------------------------------------------------------------

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
	middleware.WriteJSON(w, http.StatusOK, map[string][]categoryDTO{"categories": dtos})
}

func (h *Handler) createCategory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string  `json:"name"`
		Slug        *string `json:"slug"`
		Description string  `json:"description"`
	}
	if !h.decode(w, r, &req) {
		return
	}

	cat, err := h.content.CreateCategory(r.Context(), content.CategoryInput{
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
	})
	if err != nil {
		h.handleContentError(w, err, "Category not found.")
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]categoryDTO{"category": toCategoryDTO(cat)})
}

func (h *Handler) updateCategory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        *string `json:"name"`
		Slug        *string `json:"slug"`
		Description *string `json:"description"`
	}
	if !h.decode(w, r, &req) {
		return
	}

	cat, err := h.content.UpdateCategory(r.Context(), r.PathValue("id"), content.CategoryPatch{
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
	})
	if err != nil {
		h.handleContentError(w, err, "Category not found.")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]categoryDTO{"category": toCategoryDTO(cat)})
}

func (h *Handler) deleteCategory(w http.ResponseWriter, r *http.Request) {
	if err := h.content.DeleteCategory(r.Context(), r.PathValue("id")); err != nil {
		h.handleContentError(w, err, "Category not found.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Tags
// ---------------------------------------------------------------------------

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
	middleware.WriteJSON(w, http.StatusOK, map[string][]tagDTO{"tags": dtos})
}

func (h *Handler) createTag(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string  `json:"name"`
		Slug *string `json:"slug"`
	}
	if !h.decode(w, r, &req) {
		return
	}

	tg, err := h.content.CreateTag(r.Context(), content.TagInput{Name: req.Name, Slug: req.Slug})
	if err != nil {
		h.handleContentError(w, err, "Tag not found.")
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]tagDTO{"tag": toTagDTO(tg)})
}

func (h *Handler) updateTag(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name *string `json:"name"`
		Slug *string `json:"slug"`
	}
	if !h.decode(w, r, &req) {
		return
	}

	tg, err := h.content.UpdateTag(r.Context(), r.PathValue("id"), content.TagPatch{Name: req.Name, Slug: req.Slug})
	if err != nil {
		h.handleContentError(w, err, "Tag not found.")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]tagDTO{"tag": toTagDTO(tg)})
}

func (h *Handler) deleteTag(w http.ResponseWriter, r *http.Request) {
	if err := h.content.DeleteTag(r.Context(), r.PathValue("id")); err != nil {
		h.handleContentError(w, err, "Tag not found.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
