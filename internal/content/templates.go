package content

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"

	"github.com/pocketpress/pocketpress/internal/store"
)

const templateNameMax = 100
const templateDescriptionMax = 500

// Template is a post template aggregate: the stored row plus related
// categories, tags, and optional hydrated media.
type Template struct {
	Row        store.PostTemplate
	Categories []store.Category
	Tags       []store.Tag
	CoverImage *store.Media
	OGImage    *store.Media
}

// TemplateCreateInput is the create body for a template. FromPostID, when
// set, copies content and metadata from an existing post (name/description
// still come from this input). Otherwise the scalar fields below are used.
type TemplateCreateInput struct {
	Name           string
	Description    string
	FromPostID     string
	Title          string
	Excerpt        string
	ContentJSON    string
	ContentHTML    string
	CoverImage     string
	CategoryIDs    []string
	TagIDs         []string
	Featured       bool
	SEOTitle       string
	SEODescription string
	CanonicalURL   string
	OGImage        string
	Locale         string
	CurrentUserID  string
}

// TemplateUpdateInput is a partial update for a template. Nil pointers leave
// the field unchanged.
type TemplateUpdateInput struct {
	Name           *string
	Description    *string
	Title          *string
	Excerpt        *string
	ContentJSON    *string
	ContentHTML    *string
	CoverImage     *string
	CategoryIDs    *[]string
	TagIDs         *[]string
	Featured       *bool
	SEOTitle       *string
	SEODescription *string
	CanonicalURL   *string
	OGImage        *string
	Locale         *string
}

// ListTemplates returns every template with categories and tags hydrated.
func (s *Service) ListTemplates(ctx context.Context) ([]Template, error) {
	rows, err := store.ListPostTemplates(ctx, s.db)
	if err != nil {
		return nil, err
	}
	out := make([]Template, 0, len(rows))
	for _, row := range rows {
		t, err := s.hydrateTemplate(ctx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// GetTemplate returns one template by id.
func (s *Service) GetTemplate(ctx context.Context, id string) (Template, error) {
	row, err := store.GetPostTemplateByID(ctx, s.db, id)
	if err != nil {
		return Template{}, err
	}
	return s.hydrateTemplate(ctx, row)
}

// CreateTemplate validates and inserts a template, optionally seeded from a
// post when FromPostID is set.
func (s *Service) CreateTemplate(ctx context.Context, in TemplateCreateInput) (Template, error) {
	fields := fieldErrors{}

	name := strings.TrimSpace(in.Name)
	switch {
	case name == "":
		fields.set("name", "Name is required.")
	case runeLen(name) > templateNameMax:
		fields.set("name", "Name must be at most 100 characters.")
	}

	description := strings.TrimSpace(in.Description)
	if runeLen(description) > templateDescriptionMax {
		fields.set("description", "Description must be at most 500 characters.")
	}

	var (
		title, excerpt, contentJSON, contentHTML, contentText string
		coverImage, ogImage, seoTitle, seoDesc, canonicalURL  string
		locale                                                string
		featured                                              bool
		catIDs, tgIDs                                         []string
	)

	if in.FromPostID != "" {
		src, err := store.GetPostByID(ctx, s.db, in.FromPostID)
		if errors.Is(err, store.ErrNotFound) {
			fields.set("from_post", "Post not found.")
		} else if err != nil {
			return Template{}, err
		} else {
			srcCats, err := store.GetPostCategories(ctx, s.db, in.FromPostID)
			if err != nil {
				return Template{}, err
			}
			srcTags, err := store.GetPostTags(ctx, s.db, in.FromPostID)
			if err != nil {
				return Template{}, err
			}
			title = src.Title
			excerpt = src.Excerpt
			contentJSON = src.ContentJSON
			contentHTML = src.ContentHTML
			contentText = src.ContentText
			coverImage = src.CoverImage
			ogImage = src.OGImage
			seoTitle = src.SEOTitle
			seoDesc = src.SEODescription
			canonicalURL = src.CanonicalURL
			locale = src.Locale
			if locale == "" {
				locale = "en"
			}
			featured = src.Featured
			catIDs = categoryIDs(srcCats)
			tgIDs = tagIDs(srcTags)
		}
	} else {
		title = strings.TrimSpace(in.Title)
		if runeLen(title) > titleMax {
			fields.set("title", "Title must be at most 200 characters.")
		}
		excerpt = strings.TrimSpace(in.Excerpt)
		seoTitle = strings.TrimSpace(in.SEOTitle)
		seoDesc = strings.TrimSpace(in.SEODescription)
		canonicalURL = strings.TrimSpace(in.CanonicalURL)
		validatePostScalars(fields, excerpt, seoTitle, seoDesc, canonicalURL)

		contentJSON, contentHTML, contentText = resolveContent(in.ContentJSON, in.ContentHTML, fields)
		coverImage = strings.TrimSpace(in.CoverImage)
		ogImage = strings.TrimSpace(in.OGImage)
		featured = in.Featured
		catIDs = in.CategoryIDs
		tgIDs = in.TagIDs

		if err := s.checkCategories(ctx, catIDs, fields); err != nil {
			return Template{}, err
		}
		if err := s.checkTags(ctx, tgIDs, fields); err != nil {
			return Template{}, err
		}
		if err := s.checkMedia(ctx, coverImage, "cover_image", fields); err != nil {
			return Template{}, err
		}
		if err := s.checkMedia(ctx, ogImage, "og_image", fields); err != nil {
			return Template{}, err
		}

		resolvedLocale, err := s.resolveTemplateLocale(ctx, in.Locale, fields)
		if err != nil {
			return Template{}, err
		}
		locale = resolvedLocale
	}

	if err := fields.orNil(); err != nil {
		return Template{}, err
	}

	var newID string
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		row, err := store.CreatePostTemplate(ctx, tx, store.NewPostTemplate{
			Name:           name,
			Description:    description,
			Title:          title,
			Excerpt:        excerpt,
			ContentJSON:    contentJSON,
			ContentHTML:    contentHTML,
			ContentText:    contentText,
			CoverImage:     coverImage,
			Featured:       featured,
			SEOTitle:       seoTitle,
			SEODescription: seoDesc,
			CanonicalURL:   canonicalURL,
			OGImage:        ogImage,
			Locale:         locale,
			CreatedBy:      in.CurrentUserID,
		})
		if err != nil {
			return err
		}
		newID = row.ID
		if err := store.SetPostTemplateCategories(ctx, tx, row.ID, catIDs); err != nil {
			return err
		}
		return store.SetPostTemplateTags(ctx, tx, row.ID, tgIDs)
	})
	if err != nil {
		return Template{}, err
	}

	s.logger.Info("template created", slog.String("template_id", newID))
	return s.GetTemplate(ctx, newID)
}

// UpdateTemplate applies a partial update to a template.
func (s *Service) UpdateTemplate(ctx context.Context, id string, in TemplateUpdateInput) (Template, error) {
	existing, err := store.GetPostTemplateByID(ctx, s.db, id)
	if err != nil {
		return Template{}, err
	}
	existingCats, err := store.GetPostTemplateCategories(ctx, s.db, id)
	if err != nil {
		return Template{}, err
	}
	existingTags, err := store.GetPostTemplateTags(ctx, s.db, id)
	if err != nil {
		return Template{}, err
	}

	fields := fieldErrors{}

	name := existing.Name
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
		switch {
		case name == "":
			fields.set("name", "Name is required.")
		case runeLen(name) > templateNameMax:
			fields.set("name", "Name must be at most 100 characters.")
		}
	}

	description := existing.Description
	if in.Description != nil {
		description = strings.TrimSpace(*in.Description)
		if runeLen(description) > templateDescriptionMax {
			fields.set("description", "Description must be at most 500 characters.")
		}
	}

	title := existing.Title
	if in.Title != nil {
		title = strings.TrimSpace(*in.Title)
		if runeLen(title) > titleMax {
			fields.set("title", "Title must be at most 200 characters.")
		}
	}

	excerpt := existing.Excerpt
	if in.Excerpt != nil {
		excerpt = strings.TrimSpace(*in.Excerpt)
	}
	seoTitle := existing.SEOTitle
	if in.SEOTitle != nil {
		seoTitle = strings.TrimSpace(*in.SEOTitle)
	}
	seoDesc := existing.SEODescription
	if in.SEODescription != nil {
		seoDesc = strings.TrimSpace(*in.SEODescription)
	}
	canonicalURL := existing.CanonicalURL
	if in.CanonicalURL != nil {
		canonicalURL = strings.TrimSpace(*in.CanonicalURL)
	}
	validatePostScalars(fields, excerpt, seoTitle, seoDesc, canonicalURL)

	contentJSON := existing.ContentJSON
	contentHTML := existing.ContentHTML
	contentText := existing.ContentText
	if in.ContentJSON != nil || in.ContentHTML != nil {
		jsonIn := contentJSON
		htmlIn := contentHTML
		if in.ContentJSON != nil {
			jsonIn = *in.ContentJSON
		}
		if in.ContentHTML != nil {
			htmlIn = *in.ContentHTML
		}
		contentJSON, contentHTML, contentText = resolveContent(jsonIn, htmlIn, fields)
	}

	coverImage := existing.CoverImage
	if in.CoverImage != nil {
		coverImage = strings.TrimSpace(*in.CoverImage)
	}
	ogImage := existing.OGImage
	if in.OGImage != nil {
		ogImage = strings.TrimSpace(*in.OGImage)
	}
	if err := s.checkMedia(ctx, coverImage, "cover_image", fields); err != nil {
		return Template{}, err
	}
	if err := s.checkMedia(ctx, ogImage, "og_image", fields); err != nil {
		return Template{}, err
	}

	featured := existing.Featured
	if in.Featured != nil {
		featured = *in.Featured
	}

	catIDs := categoryIDs(existingCats)
	if in.CategoryIDs != nil {
		catIDs = *in.CategoryIDs
	}
	tgIDs := tagIDs(existingTags)
	if in.TagIDs != nil {
		tgIDs = *in.TagIDs
	}
	if err := s.checkCategories(ctx, catIDs, fields); err != nil {
		return Template{}, err
	}
	if err := s.checkTags(ctx, tgIDs, fields); err != nil {
		return Template{}, err
	}

	locale := existing.Locale
	if in.Locale != nil {
		locale, err = s.resolveTemplateLocale(ctx, *in.Locale, fields)
		if err != nil {
			return Template{}, err
		}
	}

	if err := fields.orNil(); err != nil {
		return Template{}, err
	}

	err = s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := store.UpdatePostTemplate(ctx, tx, id, store.PostTemplateUpdate{
			Name:           name,
			Description:    description,
			Title:          title,
			Excerpt:        excerpt,
			ContentJSON:    contentJSON,
			ContentHTML:    contentHTML,
			ContentText:    contentText,
			CoverImage:     coverImage,
			Featured:       featured,
			SEOTitle:       seoTitle,
			SEODescription: seoDesc,
			CanonicalURL:   canonicalURL,
			OGImage:        ogImage,
			Locale:         locale,
		}); err != nil {
			return err
		}
		if err := store.SetPostTemplateCategories(ctx, tx, id, catIDs); err != nil {
			return err
		}
		return store.SetPostTemplateTags(ctx, tx, id, tgIDs)
	})
	if err != nil {
		return Template{}, err
	}
	return s.GetTemplate(ctx, id)
}

// DeleteTemplate removes a template.
func (s *Service) DeleteTemplate(ctx context.Context, id string) error {
	return store.DeletePostTemplate(ctx, s.db, id)
}

// CreatePostFromTemplate mints a new draft post from a template. The current
// user becomes the author. Title falls back to "Untitled" when the template
// title is blank. Slug is generated uniquely from the title.
func (s *Service) CreatePostFromTemplate(ctx context.Context, templateID, currentUserID string) (Post, error) {
	tmpl, err := s.GetTemplate(ctx, templateID)
	if err != nil {
		return Post{}, err
	}

	title := strings.TrimSpace(tmpl.Row.Title)
	if title == "" {
		title = "Untitled"
	}

	return s.CreatePost(ctx, PostCreateInput{
		Title:          title,
		Excerpt:        tmpl.Row.Excerpt,
		ContentJSON:    tmpl.Row.ContentJSON,
		ContentHTML:    tmpl.Row.ContentHTML,
		CoverImage:     tmpl.Row.CoverImage,
		Status:         store.PostStatusDraft,
		AuthorID:       currentUserID,
		CategoryIDs:    categoryIDs(tmpl.Categories),
		TagIDs:         tagIDs(tmpl.Tags),
		Featured:       tmpl.Row.Featured,
		SEOTitle:       tmpl.Row.SEOTitle,
		SEODescription: tmpl.Row.SEODescription,
		CanonicalURL:   tmpl.Row.CanonicalURL,
		OGImage:        tmpl.Row.OGImage,
		Locale:         tmpl.Row.Locale,
		CurrentUserID:  currentUserID,
	})
}

func (s *Service) hydrateTemplate(ctx context.Context, row store.PostTemplate) (Template, error) {
	cats, err := store.GetPostTemplateCategories(ctx, s.db, row.ID)
	if err != nil {
		return Template{}, err
	}
	tags, err := store.GetPostTemplateTags(ctx, s.db, row.ID)
	if err != nil {
		return Template{}, err
	}
	cover, err := s.lookupMedia(ctx, row.CoverImage)
	if err != nil {
		return Template{}, err
	}
	og, err := s.lookupMedia(ctx, row.OGImage)
	if err != nil {
		return Template{}, err
	}
	return Template{
		Row:        row,
		Categories: cats,
		Tags:       tags,
		CoverImage: cover,
		OGImage:    og,
	}, nil
}

func (s *Service) resolveTemplateLocale(ctx context.Context, raw string, fields fieldErrors) (string, error) {
	settings, err := store.GetSettings(ctx, s.db)
	if err != nil {
		return "", err
	}
	available := settings.AvailableLocales
	if len(available) == 0 {
		available = []string{"en"}
	}

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return available[0], nil
	}
	normalized, ok := NormalizeLocale(raw)
	if !ok {
		fields.set("locale", "Locale must be a valid BCP 47 language tag.")
		return "", nil
	}
	if !LocaleInList(normalized, available) {
		fields.set("locale", "Locale is not in the site's available locales.")
		return "", nil
	}
	return normalized, nil
}
