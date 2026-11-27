package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PostTemplate is a row of the post_templates table (scalar columns only;
// categories and tags are loaded separately).
type PostTemplate struct {
	ID             string
	Name           string
	Description    string
	Title          string
	Excerpt        string
	ContentJSON    string
	ContentHTML    string
	ContentText    string
	CoverImage     string
	Featured       bool
	SEOTitle       string
	SEODescription string
	CanonicalURL   string
	OGImage        string
	Locale         string
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewPostTemplate holds the writable fields for CreatePostTemplate. ID and
// timestamps are set by CreatePostTemplate itself.
type NewPostTemplate struct {
	Name           string
	Description    string
	Title          string
	Excerpt        string
	ContentJSON    string
	ContentHTML    string
	ContentText    string
	CoverImage     string
	Featured       bool
	SEOTitle       string
	SEODescription string
	CanonicalURL   string
	OGImage        string
	Locale         string
	CreatedBy      string
}

// PostTemplateUpdate holds the resolved writable fields for UpdatePostTemplate.
type PostTemplateUpdate struct {
	Name           string
	Description    string
	Title          string
	Excerpt        string
	ContentJSON    string
	ContentHTML    string
	ContentText    string
	CoverImage     string
	Featured       bool
	SEOTitle       string
	SEODescription string
	CanonicalURL   string
	OGImage        string
	Locale         string
}

// CreatePostTemplate inserts a new template and returns it as stored.
func CreatePostTemplate(ctx context.Context, db DBTX, in NewPostTemplate) (PostTemplate, error) {
	id, err := newID()
	if err != nil {
		return PostTemplate{}, err
	}
	now := time.Now().UTC()
	t := PostTemplate{
		ID:             id,
		Name:           in.Name,
		Description:    in.Description,
		Title:          in.Title,
		Excerpt:        in.Excerpt,
		ContentJSON:    contentJSONOrDefault(in.ContentJSON),
		ContentHTML:    in.ContentHTML,
		ContentText:    in.ContentText,
		CoverImage:     in.CoverImage,
		Featured:       in.Featured,
		SEOTitle:       in.SEOTitle,
		SEODescription: in.SEODescription,
		CanonicalURL:   in.CanonicalURL,
		OGImage:        in.OGImage,
		Locale:         in.Locale,
		CreatedBy:      in.CreatedBy,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if t.Locale == "" {
		t.Locale = "en"
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO post_templates (
			id, name, description, title, excerpt, content_json, content_html,
			content_text, cover_image, featured, seo_title, seo_description,
			canonical_url, og_image, locale, created_by, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, nullString(t.Description), t.Title, nullString(t.Excerpt),
		t.ContentJSON, t.ContentHTML, t.ContentText, nullString(t.CoverImage),
		boolToInt(t.Featured), nullString(t.SEOTitle), nullString(t.SEODescription),
		nullString(t.CanonicalURL), nullString(t.OGImage), t.Locale, t.CreatedBy,
		formatTime(t.CreatedAt), formatTime(t.UpdatedAt),
	)
	if err != nil {
		return PostTemplate{}, fmt.Errorf("insert post template: %w", err)
	}
	return t, nil
}

// GetPostTemplateByID looks up a template by ID. Returns ErrNotFound when missing.
func GetPostTemplateByID(ctx context.Context, db DBTX, id string) (PostTemplate, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, name, description, title, excerpt, content_json, content_html,
			content_text, cover_image, featured, seo_title, seo_description,
			canonical_url, og_image, locale, created_by, created_at, updated_at
		FROM post_templates WHERE id = ?`, id)
	return scanPostTemplate(row)
}

// ListPostTemplates returns every template ordered by name (case-insensitive).
func ListPostTemplates(ctx context.Context, db DBTX) ([]PostTemplate, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, description, title, excerpt, content_json, content_html,
			content_text, cover_image, featured, seo_title, seo_description,
			canonical_url, og_image, locale, created_by, created_at, updated_at
		FROM post_templates ORDER BY name COLLATE NOCASE ASC`)
	if err != nil {
		return nil, fmt.Errorf("query post templates: %w", err)
	}
	defer rows.Close()

	out := make([]PostTemplate, 0)
	for rows.Next() {
		t, err := scanPostTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate post templates: %w", err)
	}
	return out, nil
}

// UpdatePostTemplate replaces the writable columns of a template.
func UpdatePostTemplate(ctx context.Context, db DBTX, id string, in PostTemplateUpdate) (PostTemplate, error) {
	now := time.Now().UTC()
	locale := in.Locale
	if locale == "" {
		locale = "en"
	}
	res, err := db.ExecContext(ctx, `
		UPDATE post_templates SET
			name = ?, description = ?, title = ?, excerpt = ?,
			content_json = ?, content_html = ?, content_text = ?,
			cover_image = ?, featured = ?, seo_title = ?, seo_description = ?,
			canonical_url = ?, og_image = ?, locale = ?, updated_at = ?
		WHERE id = ?`,
		in.Name, nullString(in.Description), in.Title, nullString(in.Excerpt),
		contentJSONOrDefault(in.ContentJSON), in.ContentHTML, in.ContentText,
		nullString(in.CoverImage), boolToInt(in.Featured),
		nullString(in.SEOTitle), nullString(in.SEODescription),
		nullString(in.CanonicalURL), nullString(in.OGImage), locale,
		formatTime(now), id,
	)
	if err != nil {
		return PostTemplate{}, fmt.Errorf("update post template: %w", err)
	}
	if err := requireRowsAffected(res); err != nil {
		return PostTemplate{}, err
	}
	return GetPostTemplateByID(ctx, db, id)
}

// DeletePostTemplate removes a template (join tables cascade).
func DeletePostTemplate(ctx context.Context, db DBTX, id string) error {
	res, err := db.ExecContext(ctx, `DELETE FROM post_templates WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete post template: %w", err)
	}
	return requireRowsAffected(res)
}

// SetPostTemplateCategories replaces a template's category associations.
func SetPostTemplateCategories(ctx context.Context, db DBTX, templateID string, categoryIDs []string) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM post_template_categories WHERE template_id = ?`, templateID); err != nil {
		return fmt.Errorf("clear template categories: %w", err)
	}
	for _, catID := range dedupe(categoryIDs) {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO post_template_categories (template_id, category_id) VALUES (?, ?)`,
			templateID, catID,
		); err != nil {
			return fmt.Errorf("insert template category: %w", err)
		}
	}
	return nil
}

// SetPostTemplateTags replaces a template's tag associations.
func SetPostTemplateTags(ctx context.Context, db DBTX, templateID string, tagIDs []string) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM post_template_tags WHERE template_id = ?`, templateID); err != nil {
		return fmt.Errorf("clear template tags: %w", err)
	}
	for _, tagID := range dedupe(tagIDs) {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO post_template_tags (template_id, tag_id) VALUES (?, ?)`,
			templateID, tagID,
		); err != nil {
			return fmt.Errorf("insert template tag: %w", err)
		}
	}
	return nil
}

// GetPostTemplateCategories returns the categories linked to a template,
// ordered by name.
func GetPostTemplateCategories(ctx context.Context, db DBTX, templateID string) ([]Category, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.id, c.name, c.slug, c.description, c.created_at, c.updated_at
		FROM categories c
		INNER JOIN post_template_categories tc ON tc.category_id = c.id
		WHERE tc.template_id = ?
		ORDER BY c.name COLLATE NOCASE ASC`, templateID)
	if err != nil {
		return nil, fmt.Errorf("query template categories: %w", err)
	}
	defer rows.Close()

	out := make([]Category, 0)
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate template categories: %w", err)
	}
	return out, nil
}

// GetPostTemplateTags returns the tags linked to a template, ordered by name.
func GetPostTemplateTags(ctx context.Context, db DBTX, templateID string) ([]Tag, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT t.id, t.name, t.slug, t.created_at, t.updated_at
		FROM tags t
		INNER JOIN post_template_tags tt ON tt.tag_id = t.id
		WHERE tt.template_id = ?
		ORDER BY t.name COLLATE NOCASE ASC`, templateID)
	if err != nil {
		return nil, fmt.Errorf("query template tags: %w", err)
	}
	defer rows.Close()

	out := make([]Tag, 0)
	for rows.Next() {
		tg, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, tg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate template tags: %w", err)
	}
	return out, nil
}

func scanPostTemplate(row rowScanner) (PostTemplate, error) {
	var (
		t                                     PostTemplate
		description, excerpt, cover, seoTitle sql.NullString
		seoDesc, canonical, ogImage           sql.NullString
		featured                              int
		createdAt, updatedAt                  string
	)
	err := row.Scan(
		&t.ID, &t.Name, &description, &t.Title, &excerpt, &t.ContentJSON,
		&t.ContentHTML, &t.ContentText, &cover, &featured, &seoTitle, &seoDesc,
		&canonical, &ogImage, &t.Locale, &t.CreatedBy, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PostTemplate{}, ErrNotFound
	}
	if err != nil {
		return PostTemplate{}, fmt.Errorf("scan post template: %w", err)
	}
	t.Description = stringOrEmpty(description)
	t.Excerpt = stringOrEmpty(excerpt)
	t.CoverImage = stringOrEmpty(cover)
	t.Featured = featured != 0
	t.SEOTitle = stringOrEmpty(seoTitle)
	t.SEODescription = stringOrEmpty(seoDesc)
	t.CanonicalURL = stringOrEmpty(canonical)
	t.OGImage = stringOrEmpty(ogImage)
	t.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return PostTemplate{}, err
	}
	t.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return PostTemplate{}, err
	}
	return t, nil
}
