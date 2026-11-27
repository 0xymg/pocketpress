package adminapi

import (
	"net/http"
	"time"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/http/middleware"
)

// templateDTO is the admin JSON shape for a content duplication template.
type templateDTO struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Description    string       `json:"description"`
	Title          string       `json:"title"`
	Excerpt        string       `json:"excerpt"`
	ContentJSON    string       `json:"content_json"`
	ContentHTML    string       `json:"content_html"`
	CoverImage     *mediaRefDTO `json:"cover_image"`
	Categories     []termRefDTO `json:"categories"`
	Tags           []termRefDTO `json:"tags"`
	Featured       bool         `json:"featured"`
	SEOTitle       string       `json:"seo_title"`
	SEODescription string       `json:"seo_description"`
	CanonicalURL   string       `json:"canonical_url"`
	OGImage        *mediaRefDTO `json:"og_image"`
	Locale         string       `json:"locale"`
	CreatedBy      string       `json:"created_by"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

func toTemplateDTO(t content.Template) templateDTO {
	locale := t.Row.Locale
	if locale == "" {
		locale = "en"
	}
	return templateDTO{
		ID:             t.Row.ID,
		Name:           t.Row.Name,
		Description:    t.Row.Description,
		Title:          t.Row.Title,
		Excerpt:        t.Row.Excerpt,
		ContentJSON:    t.Row.ContentJSON,
		ContentHTML:    t.Row.ContentHTML,
		CoverImage:     toMediaRefDTO(t.CoverImage),
		Categories:     categoryRefs(t.Categories),
		Tags:           tagRefs(t.Tags),
		Featured:       t.Row.Featured,
		SEOTitle:       t.Row.SEOTitle,
		SEODescription: t.Row.SEODescription,
		CanonicalURL:   t.Row.CanonicalURL,
		OGImage:        toMediaRefDTO(t.OGImage),
		Locale:         locale,
		CreatedBy:      t.Row.CreatedBy,
		CreatedAt:      t.Row.CreatedAt,
		UpdatedAt:      t.Row.UpdatedAt,
	}
}

func (h *Handler) writeTemplate(w http.ResponseWriter, status int, t content.Template) {
	middleware.WriteJSON(w, status, map[string]templateDTO{"template": toTemplateDTO(t)})
}

// listTemplates (GET /templates) returns every duplication template.
func (h *Handler) listTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := h.content.ListTemplates(r.Context())
	if err != nil {
		h.serverError(w, err)
		return
	}
	dtos := make([]templateDTO, 0, len(templates))
	for _, t := range templates {
		dtos = append(dtos, toTemplateDTO(t))
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"templates": dtos})
}

// getTemplate (GET /templates/{id}) returns one template.
func (h *Handler) getTemplate(w http.ResponseWriter, r *http.Request) {
	t, err := h.content.GetTemplate(r.Context(), r.PathValue("id"))
	if err != nil {
		h.handleContentError(w, err, "Template not found.")
		return
	}
	h.writeTemplate(w, http.StatusOK, t)
}

// createTemplate (POST /templates) creates a template from fields or from_post.
func (h *Handler) createTemplate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string   `json:"name"`
		Description    string   `json:"description"`
		FromPost       string   `json:"from_post"`
		Title          string   `json:"title"`
		Excerpt        string   `json:"excerpt"`
		ContentJSON    string   `json:"content_json"`
		ContentHTML    string   `json:"content_html"`
		CoverImage     string   `json:"cover_image"`
		Categories     []string `json:"categories"`
		Tags           []string `json:"tags"`
		Featured       bool     `json:"featured"`
		SEOTitle       string   `json:"seo_title"`
		SEODescription string   `json:"seo_description"`
		CanonicalURL   string   `json:"canonical_url"`
		OGImage        string   `json:"og_image"`
		Locale         string   `json:"locale"`
	}
	if !h.decodePost(w, r, &req) {
		return
	}
	user, _ := middleware.UserFromContext(r.Context())
	t, err := h.content.CreateTemplate(r.Context(), content.TemplateCreateInput{
		Name:           req.Name,
		Description:    req.Description,
		FromPostID:     req.FromPost,
		Title:          req.Title,
		Excerpt:        req.Excerpt,
		ContentJSON:    req.ContentJSON,
		ContentHTML:    req.ContentHTML,
		CoverImage:     req.CoverImage,
		CategoryIDs:    req.Categories,
		TagIDs:         req.Tags,
		Featured:       req.Featured,
		SEOTitle:       req.SEOTitle,
		SEODescription: req.SEODescription,
		CanonicalURL:   req.CanonicalURL,
		OGImage:        req.OGImage,
		Locale:         req.Locale,
		CurrentUserID:  user.ID,
	})
	if err != nil {
		h.handleContentError(w, err, "Template not found.")
		return
	}
	h.writeTemplate(w, http.StatusCreated, t)
}

// updateTemplate (PATCH /templates/{id}) partially updates a template.
func (h *Handler) updateTemplate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           *string   `json:"name"`
		Description    *string   `json:"description"`
		Title          *string   `json:"title"`
		Excerpt        *string   `json:"excerpt"`
		ContentJSON    *string   `json:"content_json"`
		ContentHTML    *string   `json:"content_html"`
		CoverImage     *string   `json:"cover_image"`
		Categories     *[]string `json:"categories"`
		Tags           *[]string `json:"tags"`
		Featured       *bool     `json:"featured"`
		SEOTitle       *string   `json:"seo_title"`
		SEODescription *string   `json:"seo_description"`
		CanonicalURL   *string   `json:"canonical_url"`
		OGImage        *string   `json:"og_image"`
		Locale         *string   `json:"locale"`
	}
	if !h.decodePost(w, r, &req) {
		return
	}
	t, err := h.content.UpdateTemplate(r.Context(), r.PathValue("id"), content.TemplateUpdateInput{
		Name:           req.Name,
		Description:    req.Description,
		Title:          req.Title,
		Excerpt:        req.Excerpt,
		ContentJSON:    req.ContentJSON,
		ContentHTML:    req.ContentHTML,
		CoverImage:     req.CoverImage,
		CategoryIDs:    req.Categories,
		TagIDs:         req.Tags,
		Featured:       req.Featured,
		SEOTitle:       req.SEOTitle,
		SEODescription: req.SEODescription,
		CanonicalURL:   req.CanonicalURL,
		OGImage:        req.OGImage,
		Locale:         req.Locale,
	})
	if err != nil {
		h.handleContentError(w, err, "Template not found.")
		return
	}
	h.writeTemplate(w, http.StatusOK, t)
}

// deleteTemplate (DELETE /templates/{id}) removes a template.
func (h *Handler) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	if err := h.content.DeleteTemplate(r.Context(), r.PathValue("id")); err != nil {
		h.handleContentError(w, err, "Template not found.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// createPostFromTemplate (POST /templates/{id}/create-post) mints a draft.
func (h *Handler) createPostFromTemplate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPostBodyBytes)
	user, _ := middleware.UserFromContext(r.Context())
	p, err := h.content.CreatePostFromTemplate(r.Context(), r.PathValue("id"), user.ID)
	if err != nil {
		h.handleContentError(w, err, "Template not found.")
		return
	}
	h.writePostFull(w, http.StatusCreated, p)
}
