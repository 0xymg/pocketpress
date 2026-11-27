package content

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/pocketpress/pocketpress/internal/content/markdown"
	"github.com/pocketpress/pocketpress/internal/store"
)

// Import limits for a single Markdown import request.
const (
	MaxImportDocuments = 50
)

// MarkdownImportItem is one Markdown document supplied by the admin client.
type MarkdownImportItem struct {
	Filename string
	Markdown string
}

// MarkdownImportResult is the per-document outcome of ImportMarkdown.
type MarkdownImportResult struct {
	Filename string
	Post     *Post
	Error    *ValidationError
	Message  string // non-validation failure message (safe for clients)
}

// ImportMarkdown parses Markdown documents (optional YAML frontmatter),
// converts each body to Lexical content_json + HTML, resolves/creates
// categories and tags by name, and creates posts. Each document is
// processed independently — one failure does not roll back earlier
// successes.
//
// Status defaults to draft. Frontmatter status "published" (or draft: false /
// a published_at/date) creates a draft then publishes it so webhooks fire;
// an explicit published_at/date is then written onto the post. "scheduled" /
// "archived" are rejected per document.
func (s *Service) ImportMarkdown(ctx context.Context, items []MarkdownImportItem, currentUserID string) ([]MarkdownImportResult, error) {
	if len(items) == 0 {
		return nil, &ValidationError{Fields: map[string]string{"documents": "At least one Markdown document is required."}}
	}
	if len(items) > MaxImportDocuments {
		return nil, &ValidationError{Fields: map[string]string{
			"documents": "At most 50 Markdown documents can be imported at once.",
		}}
	}
	if strings.TrimSpace(currentUserID) == "" {
		return nil, errors.New("current user id is required")
	}

	results := make([]MarkdownImportResult, 0, len(items))
	for _, item := range items {
		results = append(results, s.importOneMarkdown(ctx, item, currentUserID))
	}
	return results, nil
}

func (s *Service) importOneMarkdown(ctx context.Context, item MarkdownImportItem, currentUserID string) MarkdownImportResult {
	filename := strings.TrimSpace(item.Filename)
	res := MarkdownImportResult{Filename: filename}

	doc, err := markdown.Parse(filename, item.Markdown)
	if err != nil {
		res.Error = &ValidationError{Fields: map[string]string{"markdown": err.Error()}}
		return res
	}

	title := doc.DefaultTitle()
	body := doc.BodyWithoutLeadingH1()
	converted, err := markdown.Convert(body)
	if err != nil {
		res.Message = "Failed to convert Markdown."
		s.logger.Error("markdown convert failed", slog.String("filename", filename), slog.String("error", err.Error()))
		return res
	}

	status := doc.Meta.ResolvedStatus()
	if status == "" {
		status = store.PostStatusDraft
	}
	wantPublish := false
	switch status {
	case store.PostStatusDraft:
	case store.PostStatusPublished:
		wantPublish = true
		status = store.PostStatusDraft // create as draft, then PublishPost
	default:
		res.Error = &ValidationError{Fields: map[string]string{
			"status": "Status must be draft or published.",
		}}
		return res
	}

	catIDs, err := s.resolveOrCreateCategories(ctx, doc.Meta.Categories)
	if err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			res.Error = ve
			return res
		}
		res.Message = "Failed to resolve categories."
		s.logger.Error("markdown import categories", slog.String("error", err.Error()))
		return res
	}
	tagIDs, err := s.resolveOrCreateTags(ctx, doc.Meta.Tags)
	if err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			res.Error = ve
			return res
		}
		res.Message = "Failed to resolve tags."
		s.logger.Error("markdown import tags", slog.String("error", err.Error()))
		return res
	}

	var slugPtr *string
	if slug := strings.TrimSpace(doc.Meta.Slug); slug != "" {
		slugPtr = &slug
	}

	in := PostCreateInput{
		Title:          title,
		Slug:           slugPtr,
		Excerpt:        strings.TrimSpace(doc.Meta.Excerpt),
		ContentJSON:    converted.ContentJSON,
		ContentHTML:    converted.ContentHTML,
		Status:         status,
		CategoryIDs:    catIDs,
		TagIDs:         tagIDs,
		Featured:       doc.Meta.Featured,
		SEOTitle:       strings.TrimSpace(doc.Meta.SEOTitle),
		SEODescription: strings.TrimSpace(doc.Meta.SEODescription),
		CanonicalURL:   strings.TrimSpace(doc.Meta.CanonicalURL),
		CurrentUserID:  currentUserID,
	}

	post, err := s.CreatePost(ctx, in)
	if err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			res.Error = ve
			return res
		}
		res.Message = "Failed to create post."
		s.logger.Error("markdown import create", slog.String("filename", filename), slog.String("error", err.Error()))
		return res
	}

	if wantPublish {
		published, err := s.PublishPost(ctx, post.Row.ID)
		if err != nil {
			// Post exists as draft; report publish failure but still return the post.
			s.logger.Error("markdown import publish", slog.String("post_id", post.Row.ID), slog.String("error", err.Error()))
			res.Post = &post
			res.Message = "Post created as draft, but publishing failed."
			return res
		}
		post = published

		// Honor an explicit historical publish time from frontmatter after
		// PublishPost has set status=published and fired webhooks.
		if doc.Meta.PublishedAt != nil {
			at := doc.Meta.PublishedAt.UTC()
			if err := store.SetPostStatus(ctx, s.db, post.Row.ID, store.PostStatusPublished, &at, nil); err != nil {
				s.logger.Error("markdown import published_at", slog.String("post_id", post.Row.ID), slog.String("error", err.Error()))
				res.Post = &post
				res.Message = "Post published, but published_at could not be set from frontmatter."
				return res
			}
			reloaded, err := s.loadPost(ctx, post.Row.ID)
			if err != nil {
				res.Post = &post
				return res
			}
			post = reloaded
		}
	}

	res.Post = &post
	s.logger.Info("markdown imported",
		slog.String("filename", filename),
		slog.String("post_id", post.Row.ID),
		slog.String("status", post.Row.Status),
	)
	return res
}

func (s *Service) resolveOrCreateCategories(ctx context.Context, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		slug := slugifyOrEmpty(name)
		if slug == "" {
			return nil, &ValidationError{Fields: map[string]string{
				"categories": "Category name " + name + " produces an empty slug.",
			}}
		}
		existing, err := store.GetCategoryBySlug(ctx, s.db, slug)
		if err == nil {
			ids = append(ids, existing.ID)
			continue
		}
		if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		created, err := s.CreateCategory(ctx, CategoryInput{Name: name, Slug: &slug})
		if err != nil {
			return nil, err
		}
		ids = append(ids, created.ID)
	}
	return ids, nil
}

func (s *Service) resolveOrCreateTags(ctx context.Context, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		slug := slugifyOrEmpty(name)
		if slug == "" {
			return nil, &ValidationError{Fields: map[string]string{
				"tags": "Tag name " + name + " produces an empty slug.",
			}}
		}
		existing, err := store.GetTagBySlug(ctx, s.db, slug)
		if err == nil {
			ids = append(ids, existing.ID)
			continue
		}
		if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		created, err := s.CreateTag(ctx, TagInput{Name: name, Slug: &slug})
		if err != nil {
			return nil, err
		}
		ids = append(ids, created.ID)
	}
	return ids, nil
}

func slugifyOrEmpty(name string) string {
	return Slugify(name)
}

// IsMarkdownFilename reports whether name looks like a Markdown file.
func IsMarkdownFilename(name string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name)))
	return ext == ".md" || ext == ".markdown"
}
