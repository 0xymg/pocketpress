package content

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/pocketpress/pocketpress/internal/content/ghost"
	"github.com/pocketpress/pocketpress/internal/content/htmlconv"
	"github.com/pocketpress/pocketpress/internal/content/wordpress"
	"github.com/pocketpress/pocketpress/internal/store"
)

// ExportImportResult is the per-post outcome of ImportGhost / ImportWordPress.
type ExportImportResult struct {
	SourceKey string
	Post      *Post
	Error     *ValidationError
	Message   string
}

// exportDoc is the shared intermediate shape for Ghost and WordPress posts.
type exportDoc struct {
	SourceKey      string
	Title          string
	Slug           string
	Excerpt        string
	HTML           string
	LexicalJSON    string
	Status         string
	PublishedAt    *time.Time
	Featured       bool
	Tags           []string
	Categories     []string
	SEOTitle       string
	SEODescription string
	CanonicalURL   string
}

// ImportGhost parses a Ghost JSON export and creates posts. Each post is
// processed independently — one failure does not roll back earlier successes.
func (s *Service) ImportGhost(ctx context.Context, raw []byte, currentUserID string) ([]ExportImportResult, error) {
	if strings.TrimSpace(currentUserID) == "" {
		return nil, errors.New("current user id is required")
	}
	docs, err := ghost.Parse(raw)
	if err != nil {
		return nil, &ValidationError{Fields: map[string]string{"file": err.Error()}}
	}
	items := make([]exportDoc, 0, len(docs))
	for _, d := range docs {
		items = append(items, exportDoc{
			SourceKey:      d.SourceKey,
			Title:          d.Title,
			Slug:           d.Slug,
			Excerpt:        d.Excerpt,
			HTML:           d.HTML,
			LexicalJSON:    d.LexicalJSON,
			Status:         d.Status,
			PublishedAt:    d.PublishedAt,
			Featured:       d.Featured,
			Tags:           d.Tags,
			SEOTitle:       d.SEOTitle,
			SEODescription: d.SEODescription,
			CanonicalURL:   d.CanonicalURL,
		})
	}
	return s.importExportDocs(ctx, items, currentUserID, "ghost"), nil
}

// ImportWordPress parses a WordPress WXR export and creates posts. Each post
// is processed independently — one failure does not roll back earlier successes.
func (s *Service) ImportWordPress(ctx context.Context, raw []byte, currentUserID string) ([]ExportImportResult, error) {
	if strings.TrimSpace(currentUserID) == "" {
		return nil, errors.New("current user id is required")
	}
	docs, err := wordpress.Parse(raw)
	if err != nil {
		return nil, &ValidationError{Fields: map[string]string{"file": err.Error()}}
	}
	items := make([]exportDoc, 0, len(docs))
	for _, d := range docs {
		items = append(items, exportDoc{
			SourceKey:      d.SourceKey,
			Title:          d.Title,
			Slug:           d.Slug,
			Excerpt:        d.Excerpt,
			HTML:           d.HTML,
			Status:         d.Status,
			PublishedAt:    d.PublishedAt,
			Featured:       d.Featured,
			Tags:           d.Tags,
			Categories:     d.Categories,
			SEOTitle:       d.SEOTitle,
			SEODescription: d.SEODescription,
			CanonicalURL:   d.CanonicalURL,
		})
	}
	return s.importExportDocs(ctx, items, currentUserID, "wordpress"), nil
}

func (s *Service) importExportDocs(ctx context.Context, items []exportDoc, currentUserID, source string) []ExportImportResult {
	results := make([]ExportImportResult, 0, len(items))
	for _, item := range items {
		results = append(results, s.importOneExportDoc(ctx, item, currentUserID, source))
	}
	return results
}

func (s *Service) importOneExportDoc(ctx context.Context, doc exportDoc, currentUserID, source string) ExportImportResult {
	res := ExportImportResult{SourceKey: doc.SourceKey}

	contentJSON, contentHTML, err := resolveExportContent(doc)
	if err != nil {
		res.Message = "Failed to convert post content."
		s.logger.Error(source+" import convert", slog.String("source_key", doc.SourceKey), slog.String("error", err.Error()))
		return res
	}

	status := doc.Status
	if status == "" {
		status = store.PostStatusDraft
	}
	wantPublish := false
	switch status {
	case store.PostStatusDraft:
	case store.PostStatusPublished:
		wantPublish = true
		status = store.PostStatusDraft
	default:
		res.Error = &ValidationError{Fields: map[string]string{
			"status": "Status must be draft or published.",
		}}
		return res
	}

	catIDs, err := s.resolveOrCreateCategories(ctx, doc.Categories)
	if err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			res.Error = ve
			return res
		}
		res.Message = "Failed to resolve categories."
		s.logger.Error(source+" import categories", slog.String("error", err.Error()))
		return res
	}
	tagIDs, err := s.resolveOrCreateTags(ctx, doc.Tags)
	if err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			res.Error = ve
			return res
		}
		res.Message = "Failed to resolve tags."
		s.logger.Error(source+" import tags", slog.String("error", err.Error()))
		return res
	}

	var slugPtr *string
	if slug := strings.TrimSpace(doc.Slug); slug != "" {
		slugPtr = &slug
	}

	title := strings.TrimSpace(doc.Title)
	if title == "" {
		title = "Untitled"
	}

	in := PostCreateInput{
		Title:          title,
		Slug:           slugPtr,
		Excerpt:        strings.TrimSpace(doc.Excerpt),
		ContentJSON:    contentJSON,
		ContentHTML:    contentHTML,
		Status:         status,
		CategoryIDs:    catIDs,
		TagIDs:         tagIDs,
		Featured:       doc.Featured,
		SEOTitle:       strings.TrimSpace(doc.SEOTitle),
		SEODescription: strings.TrimSpace(doc.SEODescription),
		CanonicalURL:   strings.TrimSpace(doc.CanonicalURL),
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
		s.logger.Error(source+" import create", slog.String("source_key", doc.SourceKey), slog.String("error", err.Error()))
		return res
	}

	if wantPublish {
		published, err := s.PublishPost(ctx, post.Row.ID)
		if err != nil {
			s.logger.Error(source+" import publish", slog.String("post_id", post.Row.ID), slog.String("error", err.Error()))
			res.Post = &post
			res.Message = "Post created as draft, but publishing failed."
			return res
		}
		post = published

		if doc.PublishedAt != nil {
			at := doc.PublishedAt.UTC()
			if err := store.SetPostStatus(ctx, s.db, post.Row.ID, store.PostStatusPublished, &at, nil); err != nil {
				s.logger.Error(source+" import published_at", slog.String("post_id", post.Row.ID), slog.String("error", err.Error()))
				res.Post = &post
				res.Message = "Post published, but published_at could not be set from the export."
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
	s.logger.Info(source+" imported",
		slog.String("source_key", doc.SourceKey),
		slog.String("post_id", post.Row.ID),
		slog.String("status", post.Row.Status),
	)
	return res
}

func resolveExportContent(doc exportDoc) (contentJSON, contentHTML string, err error) {
	// Prefer Ghost's Lexical JSON when it looks like an editor state; always
	// keep the export HTML for storage (CreatePost sanitizes it).
	if htmlconv.ValidLexicalJSON(doc.LexicalJSON) {
		return doc.LexicalJSON, doc.HTML, nil
	}
	converted, err := htmlconv.Convert(doc.HTML)
	if err != nil {
		return "", "", err
	}
	return converted.ContentJSON, converted.ContentHTML, nil
}
