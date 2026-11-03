package publicapi

import (
	"net/url"
	"time"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/store"
)

// metaDTO is the public API's list pagination envelope.
type metaDTO struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	TotalItems int `json:"total_items"`
	TotalPages int `json:"total_pages"`
}

// authorDTO is the minimal {id,name} author projection embedded in a post
// — never email or role.
type authorDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// termDTO is the compact {id,name,slug} shape used for a post's
// categories and tags.
type termDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// coverImageDTO is a post's cover image. A nil *coverImageDTO serializes as
// JSON null (no cover image set).
type coverImageDTO struct {
	URL    string `json:"url"`
	Width  *int   `json:"width"`
	Height *int   `json:"height"`
	Alt    string `json:"alt"`
}

// seoDTO holds a post's SEO metadata. Each field is null when the
// underlying post field is empty, rather than an empty string.
type seoDTO struct {
	Title        *string `json:"title"`
	Description  *string `json:"description"`
	CanonicalURL *string `json:"canonical_url"`
	OGImageURL   *string `json:"og_image_url"`
}

// postDTO is the frozen public POST shape (development plan section 6).
// It deliberately never carries status, content_json, content_text,
// scheduled_for or any other admin-only/internal field.
type postDTO struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Slug        string         `json:"slug"`
	Excerpt     string         `json:"excerpt"`
	ContentHTML string         `json:"content_html"`
	CoverImage  *coverImageDTO `json:"cover_image"`
	Author      authorDTO      `json:"author"`
	Categories  []termDTO      `json:"categories"`
	Tags        []termDTO      `json:"tags"`
	Featured    bool           `json:"featured"`
	SEO         seoDTO         `json:"seo"`
	PublishedAt *time.Time     `json:"published_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// toPostDTO builds the public post shape from a hydrated content.Post. For a
// published post p.Row.PublishedAt is set and serializes as an RFC3339
// timestamp; for a draft served through the authorized preview endpoint it is
// nil and serializes as JSON null. Both the published detail endpoint and the
// preview endpoint share this serializer, so it tolerates a nil published_at.
func toPostDTO(p content.Post) postDTO {
	return postDTO{
		ID:          p.Row.ID,
		Title:       p.Row.Title,
		Slug:        p.Row.Slug,
		Excerpt:     p.Row.Excerpt,
		ContentHTML: p.Row.ContentHTML,
		CoverImage:  toCoverImageDTO(p.CoverImage),
		Author:      authorDTO{ID: p.Author.ID, Name: p.Author.Name},
		Categories:  toTermDTOs(p.Categories),
		Tags:        toTagTermDTOs(p.Tags),
		Featured:    p.Row.Featured,
		SEO: seoDTO{
			Title:        stringOrNil(p.Row.SEOTitle),
			Description:  stringOrNil(p.Row.SEODescription),
			CanonicalURL: stringOrNil(p.Row.CanonicalURL),
			OGImageURL:   mediaURLOrNil(p.OGImage),
		},
		PublishedAt: p.Row.PublishedAt,
		UpdatedAt:   p.Row.UpdatedAt,
	}
}

func toCoverImageDTO(m *store.Media) *coverImageDTO {
	if m == nil {
		return nil
	}
	return &coverImageDTO{
		URL:    fileURL(m.ID, m.OriginalName),
		Width:  m.Width,
		Height: m.Height,
		Alt:    m.Alt,
	}
}

func toTermDTOs(cats []store.Category) []termDTO {
	terms := make([]termDTO, 0, len(cats))
	for _, c := range cats {
		terms = append(terms, termDTO{ID: c.ID, Name: c.Name, Slug: c.Slug})
	}
	return terms
}

func toTagTermDTOs(tags []store.Tag) []termDTO {
	terms := make([]termDTO, 0, len(tags))
	for _, t := range tags {
		terms = append(terms, termDTO{ID: t.ID, Name: t.Name, Slug: t.Slug})
	}
	return terms
}

// categoryDTO is the public CATEGORY shape: {id,name,slug,description}.
type categoryDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

func toCategoryDTO(c store.Category) categoryDTO {
	return categoryDTO{ID: c.ID, Name: c.Name, Slug: c.Slug, Description: c.Description}
}

// tagDTO is the public TAG shape: {id,name,slug} (no description).
type tagDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func toTagDTO(t store.Tag) tagDTO {
	return tagDTO{ID: t.ID, Name: t.Name, Slug: t.Slug}
}

// fileURL builds the relative public URL for a stored media object, e.g.
// "/api/files/{id}/{name}" — the same GET /api/files/{id}/{name...} route
// the admin API's media serving uses. The url-escaped original name riding
// along after the id is purely cosmetic (a human- and
// browser-download-friendly filename); only the id resolves the file.
func fileURL(id, originalName string) string {
	return "/api/files/" + id + "/" + url.PathEscape(originalName)
}

// mediaURLOrNil returns m's public file URL, or nil when m is unset.
func mediaURLOrNil(m *store.Media) *string {
	if m == nil {
		return nil
	}
	u := fileURL(m.ID, m.OriginalName)
	return &u
}

// stringOrNil returns nil for an empty string, and a pointer to s
// otherwise — how optional post scalar fields (seo_title, seo_description,
// canonical_url, ...) serialize as JSON null vs. a string.
func stringOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
