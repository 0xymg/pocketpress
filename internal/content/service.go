package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pocketpress/pocketpress/internal/store"
)

// Field length limits (per development plan §5). Counted in characters
// (runes), not bytes.
const (
	titleMax            = 200
	excerptMax          = 500
	seoTitleMax         = 70
	seoDescriptionMax   = 170
	categoryNameMax     = 100
	tagNameMax          = 100
	maxRevisionsPerPost = 50
)

// Content size caps. content_json and content_html are each limited to 2 MiB
// of raw request bytes; the surrounding request body cap (5 MiB) is enforced
// at the HTTP layer.
const (
	contentJSONMax = 2 << 20 // 2 MiB
	contentHTMLMax = 2 << 20 // 2 MiB
)

// emptyContentJSON is what a blank content_json normalizes to before storage
// (mirrors the store-side placeholder for the NOT NULL column).
const emptyContentJSON = "{}"

// Frozen validation messages. The slug and status messages are contractual
// (the admin UI is built against them); the length messages are ours.
const (
	slugEmptyMessage     = "slug cannot be empty"
	slugConflictMessage  = "This slug is already in use."
	statusPublishMessage = "Publishing arrives in a later phase."
	statusInvalidMessage = "Status must be 'draft' or 'archived'."
)

// Content validation messages (ours).
const (
	contentJSONInvalidMessage  = "Content JSON must be valid JSON."
	contentJSONTooLargeMessage = "Content JSON must be at most 2 MB."
	contentHTMLTooLargeMessage = "Content HTML must be at most 2 MB."
)

// Service is the content domain layer over the store repositories. It owns
// validation, slug normalization/uniqueness, the Phase 3 status guard,
// content-HTML derivation and revision bookkeeping. It is safe for
// concurrent use (it holds no per-request state).
type Service struct {
	db     *sql.DB
	logger *slog.Logger
	events Events
}

// NewService builds a content Service over db. logger may be nil. The event
// sink defaults to NoopEvents; install a real one with SetEvents.
func NewService(db *sql.DB, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{db: db, logger: logger, events: NoopEvents{}}
}

// SetEvents installs the outbound event sink (see Events). Passing nil is
// ignored, so the service always has a usable sink. Phase 7 uses this to
// wire the webhook dispatcher; Phase 6 leaves the default NoopEvents in
// place.
func (s *Service) SetEvents(e Events) {
	if e != nil {
		s.events = e
	}
}

// slugExistsFunc is a repository "is this slug taken (excluding excludeID)"
// check; resolveSlug/resolveUpdatedSlug take one so the same slug logic
// serves posts, categories and tags.
type slugExistsFunc func(ctx context.Context, db store.DBTX, slug, excludeID string) (bool, error)

// ---------------------------------------------------------------------------
// Categories
// ---------------------------------------------------------------------------

// ListCategories returns every category, ordered by name.
func (s *Service) ListCategories(ctx context.Context) ([]store.Category, error) {
	return store.ListCategories(ctx, s.db)
}

// CreateCategory validates and inserts a category. On validation failure it
// returns a *ValidationError.
func (s *Service) CreateCategory(ctx context.Context, in CategoryInput) (store.Category, error) {
	fields := fieldErrors{}
	name := strings.TrimSpace(in.Name)
	switch {
	case name == "":
		fields.set("name", "Name is required.")
	case runeLen(name) > categoryNameMax:
		fields.set("name", "Name must be at most 100 characters.")
	}

	slug, err := s.resolveSlug(ctx, in.Slug, name, "", fields, store.CategorySlugExists)
	if err != nil {
		return store.Category{}, err
	}
	if err := fields.orNil(); err != nil {
		return store.Category{}, err
	}

	cat, err := store.CreateCategory(ctx, s.db, store.NewCategory{
		Name:        name,
		Slug:        slug,
		Description: strings.TrimSpace(in.Description),
	})
	if err != nil {
		return store.Category{}, err
	}
	s.logger.Info("category created", slog.String("category_id", cat.ID))
	return cat, nil
}

// UpdateCategory applies a partial update. It returns store.ErrNotFound for
// an unknown id and a *ValidationError on validation failure.
func (s *Service) UpdateCategory(ctx context.Context, id string, patch CategoryPatch) (store.Category, error) {
	existing, err := store.GetCategoryByID(ctx, s.db, id)
	if err != nil {
		return store.Category{}, err
	}

	fields := fieldErrors{}
	name := existing.Name
	if patch.Name != nil {
		name = strings.TrimSpace(*patch.Name)
		switch {
		case name == "":
			fields.set("name", "Name is required.")
		case runeLen(name) > categoryNameMax:
			fields.set("name", "Name must be at most 100 characters.")
		}
	}

	slug, err := s.resolveUpdatedSlug(ctx, patch.Slug, existing.Slug, id, fields, store.CategorySlugExists)
	if err != nil {
		return store.Category{}, err
	}

	description := existing.Description
	if patch.Description != nil {
		description = strings.TrimSpace(*patch.Description)
	}

	if err := fields.orNil(); err != nil {
		return store.Category{}, err
	}

	if err := store.UpdateCategory(ctx, s.db, id, name, slug, description); err != nil {
		return store.Category{}, err
	}
	return store.GetCategoryByID(ctx, s.db, id)
}

// DeleteCategory removes a category. Returns store.ErrNotFound for an
// unknown id.
func (s *Service) DeleteCategory(ctx context.Context, id string) error {
	return store.DeleteCategory(ctx, s.db, id)
}

// ---------------------------------------------------------------------------
// Tags
// ---------------------------------------------------------------------------

// ListTags returns every tag, ordered by name.
func (s *Service) ListTags(ctx context.Context) ([]store.Tag, error) {
	return store.ListTags(ctx, s.db)
}

// CreateTag validates and inserts a tag.
func (s *Service) CreateTag(ctx context.Context, in TagInput) (store.Tag, error) {
	fields := fieldErrors{}
	name := strings.TrimSpace(in.Name)
	switch {
	case name == "":
		fields.set("name", "Name is required.")
	case runeLen(name) > tagNameMax:
		fields.set("name", "Name must be at most 100 characters.")
	}

	slug, err := s.resolveSlug(ctx, in.Slug, name, "", fields, store.TagSlugExists)
	if err != nil {
		return store.Tag{}, err
	}
	if err := fields.orNil(); err != nil {
		return store.Tag{}, err
	}

	tg, err := store.CreateTag(ctx, s.db, store.NewTag{Name: name, Slug: slug})
	if err != nil {
		return store.Tag{}, err
	}
	s.logger.Info("tag created", slog.String("tag_id", tg.ID))
	return tg, nil
}

// UpdateTag applies a partial update to a tag.
func (s *Service) UpdateTag(ctx context.Context, id string, patch TagPatch) (store.Tag, error) {
	existing, err := store.GetTagByID(ctx, s.db, id)
	if err != nil {
		return store.Tag{}, err
	}

	fields := fieldErrors{}
	name := existing.Name
	if patch.Name != nil {
		name = strings.TrimSpace(*patch.Name)
		switch {
		case name == "":
			fields.set("name", "Name is required.")
		case runeLen(name) > tagNameMax:
			fields.set("name", "Name must be at most 100 characters.")
		}
	}

	slug, err := s.resolveUpdatedSlug(ctx, patch.Slug, existing.Slug, id, fields, store.TagSlugExists)
	if err != nil {
		return store.Tag{}, err
	}

	if err := fields.orNil(); err != nil {
		return store.Tag{}, err
	}

	if err := store.UpdateTag(ctx, s.db, id, name, slug); err != nil {
		return store.Tag{}, err
	}
	return store.GetTagByID(ctx, s.db, id)
}

// DeleteTag removes a tag. Returns store.ErrNotFound for an unknown id.
func (s *Service) DeleteTag(ctx context.Context, id string) error {
	return store.DeleteTag(ctx, s.db, id)
}

// ---------------------------------------------------------------------------
// Authors
// ---------------------------------------------------------------------------

// ListAuthors returns the id and name of every active user, for the admin
// author selector. Email and role are deliberately not exposed here.
func (s *Service) ListAuthors(ctx context.Context) ([]Author, error) {
	users, err := store.ListUsers(ctx, s.db)
	if err != nil {
		return nil, err
	}
	authors := make([]Author, 0, len(users))
	for _, u := range users {
		if !u.Active {
			continue
		}
		authors = append(authors, Author{ID: u.ID, Name: u.Name})
	}
	return authors, nil
}

// ---------------------------------------------------------------------------
// Posts
// ---------------------------------------------------------------------------

// PostListQuery is the resolved (already clamped/normalized by the handler)
// query for ListPosts.
type PostListQuery struct {
	Page       int
	PerPage    int
	Status     string
	CategoryID string
	Search     string
	// Sort selects the column to order by. The zero value (PostSortUpdatedAt)
	// preserves the admin list's long-standing default.
	Sort     store.PostSortField
	SortDesc bool
}

// PostListResult is a page of posts plus its pagination metadata.
type PostListResult struct {
	Posts      []Post
	Page       int
	PerPage    int
	TotalItems int
	TotalPages int
}

// ListPosts returns one page of posts matching q, hydrated with their
// author, categories and tags.
func (s *Service) ListPosts(ctx context.Context, q PostListQuery) (PostListResult, error) {
	filter := store.PostFilter{Status: q.Status, CategoryID: q.CategoryID, Search: q.Search}

	total, err := store.CountPosts(ctx, s.db, filter)
	if err != nil {
		return PostListResult{}, err
	}

	offset := (q.Page - 1) * q.PerPage
	rows, err := store.ListPosts(ctx, s.db, filter, q.Sort, q.SortDesc, q.PerPage, offset)
	if err != nil {
		return PostListResult{}, err
	}

	authorCache := map[string]store.User{}
	posts := make([]Post, 0, len(rows))
	for _, row := range rows {
		p, err := s.hydrate(ctx, row, authorCache)
		if err != nil {
			return PostListResult{}, err
		}
		posts = append(posts, p)
	}

	totalPages := 0
	if q.PerPage > 0 {
		totalPages = (total + q.PerPage - 1) / q.PerPage
	}

	return PostListResult{
		Posts:      posts,
		Page:       q.Page,
		PerPage:    q.PerPage,
		TotalItems: total,
		TotalPages: totalPages,
	}, nil
}

// GetPost returns a single post aggregate. Returns store.ErrNotFound for an
// unknown id.
func (s *Service) GetPost(ctx context.Context, id string) (Post, error) {
	return s.loadPost(ctx, id)
}

// CreatePost validates and inserts a post, writing revision #1. The row,
// its category/tag joins and its first revision are written in one
// transaction.
func (s *Service) CreatePost(ctx context.Context, in PostCreateInput) (Post, error) {
	fields := fieldErrors{}

	title := strings.TrimSpace(in.Title)
	switch {
	case title == "":
		fields.set("title", "Title is required.")
	case runeLen(title) > titleMax:
		fields.set("title", "Title must be at most 200 characters.")
	}

	slug, err := s.resolveSlug(ctx, in.Slug, title, "", fields, store.PostSlugExists)
	if err != nil {
		return Post{}, err
	}

	excerpt := strings.TrimSpace(in.Excerpt)
	seoTitle := strings.TrimSpace(in.SEOTitle)
	seoDesc := strings.TrimSpace(in.SEODescription)
	canonicalURL := strings.TrimSpace(in.CanonicalURL)
	validatePostScalars(fields, excerpt, seoTitle, seoDesc, canonicalURL)

	status := store.PostStatusDraft
	if in.Status != "" {
		status = resolveWritableStatus(in.Status, fields)
	}

	author := strings.TrimSpace(in.AuthorID)
	if author == "" {
		author = in.CurrentUserID
	}
	if err := s.checkAuthor(ctx, author, fields); err != nil {
		return Post{}, err
	}
	if err := s.checkCategories(ctx, in.CategoryIDs, fields); err != nil {
		return Post{}, err
	}
	if err := s.checkTags(ctx, in.TagIDs, fields); err != nil {
		return Post{}, err
	}

	coverImage := strings.TrimSpace(in.CoverImage)
	if err := s.checkMedia(ctx, coverImage, "cover_image", fields); err != nil {
		return Post{}, err
	}
	ogImage := strings.TrimSpace(in.OGImage)
	if err := s.checkMedia(ctx, ogImage, "og_image", fields); err != nil {
		return Post{}, err
	}

	contentJSON, contentHTML, contentText := resolveContent(in.ContentJSON, in.ContentHTML, fields)

	if err := fields.orNil(); err != nil {
		return Post{}, err
	}

	var newID string
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		p, err := store.CreatePost(ctx, tx, store.NewPost{
			Title:          title,
			Slug:           slug,
			Excerpt:        excerpt,
			ContentJSON:    contentJSON,
			ContentHTML:    contentHTML,
			ContentText:    contentText,
			CoverImage:     coverImage,
			Status:         status,
			Author:         author,
			Featured:       in.Featured,
			SEOTitle:       seoTitle,
			SEODescription: seoDesc,
			CanonicalURL:   canonicalURL,
			OGImage:        ogImage,
		})
		if err != nil {
			return err
		}
		newID = p.ID
		if err := store.SetPostCategories(ctx, tx, p.ID, in.CategoryIDs); err != nil {
			return err
		}
		if err := store.SetPostTags(ctx, tx, p.ID, in.TagIDs); err != nil {
			return err
		}
		if _, err := store.CreateRevision(ctx, tx, store.NewRevision{
			PostID:      p.ID,
			Title:       title,
			Excerpt:     excerpt,
			ContentJSON: contentJSON,
			ContentHTML: contentHTML,
			EditorID:    in.CurrentUserID,
		}); err != nil {
			return err
		}
		_, err = store.PruneRevisions(ctx, tx, p.ID, maxRevisionsPerPost)
		return err
	})
	if err != nil {
		return Post{}, err
	}

	s.logger.Info("post created", slog.String("post_id", newID), slog.String("author_id", author))
	return s.loadPost(ctx, newID)
}

// UpdatePost applies a partial update, running the revision logic. Returns
// store.ErrNotFound for an unknown id and a *ValidationError on validation
// failure.
func (s *Service) UpdatePost(ctx context.Context, id string, in PostUpdateInput) (Post, error) {
	existing, err := store.GetPostByID(ctx, s.db, id)
	if err != nil {
		return Post{}, err
	}

	fields := fieldErrors{}

	title := existing.Title
	if in.Title != nil {
		title = strings.TrimSpace(*in.Title)
		switch {
		case title == "":
			fields.set("title", "Title is required.")
		case runeLen(title) > titleMax:
			fields.set("title", "Title must be at most 200 characters.")
		}
	}

	slug, err := s.resolveUpdatedSlug(ctx, in.Slug, existing.Slug, id, fields, store.PostSlugExists)
	if err != nil {
		return Post{}, err
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

	// content_json and content_html are independent partial fields; an omitted
	// one falls back to the stored value. The stored HTML is already
	// sanitized, so re-running resolveContent over it is idempotent.
	rawJSON := existing.ContentJSON
	if in.ContentJSON != nil {
		rawJSON = *in.ContentJSON
	}
	rawHTML := existing.ContentHTML
	if in.ContentHTML != nil {
		rawHTML = *in.ContentHTML
	}
	contentJSON, contentHTML, contentText := resolveContent(rawJSON, rawHTML, fields)

	// A normal update never changes status: the publish/unpublish/schedule/
	// archive operations own every status transition (and the published_at /
	// scheduled_for columns that go with it). Saving a published post keeps
	// it published — that is how the editor's "Save" works on live content.
	// The request's status field, if any, is ignored here.
	status := existing.Status

	author := existing.Author
	if in.AuthorID != nil {
		author = strings.TrimSpace(*in.AuthorID)
		if author == "" {
			fields.set("author", "Unknown author.")
		} else if err := s.checkAuthor(ctx, author, fields); err != nil {
			return Post{}, err
		}
	}

	featured := existing.Featured
	if in.Featured != nil {
		featured = *in.Featured
	}

	if in.CategoryIDs != nil {
		if err := s.checkCategories(ctx, *in.CategoryIDs, fields); err != nil {
			return Post{}, err
		}
	}
	if in.TagIDs != nil {
		if err := s.checkTags(ctx, *in.TagIDs, fields); err != nil {
			return Post{}, err
		}
	}

	coverImage := existing.CoverImage
	if in.CoverImage != nil {
		coverImage = strings.TrimSpace(*in.CoverImage)
		if err := s.checkMedia(ctx, coverImage, "cover_image", fields); err != nil {
			return Post{}, err
		}
	}
	ogImage := existing.OGImage
	if in.OGImage != nil {
		ogImage = strings.TrimSpace(*in.OGImage)
		if err := s.checkMedia(ctx, ogImage, "og_image", fields); err != nil {
			return Post{}, err
		}
	}

	if err := fields.orNil(); err != nil {
		return Post{}, err
	}

	err = s.inTx(ctx, func(tx *sql.Tx) error {
		if err := store.UpdatePost(ctx, tx, id, store.PostUpdate{
			Title:          title,
			Slug:           slug,
			Excerpt:        excerpt,
			ContentJSON:    contentJSON,
			ContentHTML:    contentHTML,
			ContentText:    contentText,
			CoverImage:     coverImage,
			Status:         status,
			Author:         author,
			Featured:       featured,
			SEOTitle:       seoTitle,
			SEODescription: seoDesc,
			CanonicalURL:   canonicalURL,
			OGImage:        ogImage,
		}); err != nil {
			return err
		}
		if in.CategoryIDs != nil {
			if err := store.SetPostCategories(ctx, tx, id, *in.CategoryIDs); err != nil {
				return err
			}
		}
		if in.TagIDs != nil {
			if err := store.SetPostTags(ctx, tx, id, *in.TagIDs); err != nil {
				return err
			}
		}
		return s.maybeCreateRevision(ctx, tx, id, title, excerpt, contentJSON, contentHTML, in.CurrentUserID)
	})
	if err != nil {
		return Post{}, err
	}

	s.logger.Info("post updated", slog.String("post_id", id))
	p, err := s.loadPost(ctx, id)
	if err != nil {
		return Post{}, err
	}
	// A live (published) post's content changed — let the frontend know so it
	// can rebuild. Draft/scheduled/archived edits are not publicly visible, so
	// they fire nothing.
	if p.Row.Status == store.PostStatusPublished {
		s.events.PostUpdated(ctx, &p)
	}
	return p, nil
}

// DeletePost removes a post (revisions and joins cascade). Returns
// store.ErrNotFound for an unknown id. Deleting a published post fires
// PostDeleted so the frontend can drop it.
func (s *Service) DeletePost(ctx context.Context, id string) error {
	// Load the aggregate before deleting so the event carries the post that
	// was removed and so we know whether it was published (only published
	// posts were publicly visible and therefore worth a delete event).
	p, err := s.loadPost(ctx, id)
	if err != nil {
		return err
	}
	if err := store.DeletePost(ctx, s.db, id); err != nil {
		return err
	}
	s.logger.Info("post deleted", slog.String("post_id", id))
	if p.Row.Status == store.PostStatusPublished {
		s.events.PostDeleted(ctx, &p)
	}
	return nil
}

// DuplicatePost copies a post into a fresh draft: title "Copy of <title>"
// (capped at 200), status draft, a unique "-copy" slug, and cleared
// publish/schedule timestamps (Phase 3 never sets those anyway). Categories,
// tags and the body are copied; revision #1 is written for the copy.
func (s *Service) DuplicatePost(ctx context.Context, id, currentUserID string) (Post, error) {
	src, err := store.GetPostByID(ctx, s.db, id)
	if err != nil {
		return Post{}, err
	}
	srcCats, err := store.GetPostCategories(ctx, s.db, id)
	if err != nil {
		return Post{}, err
	}
	srcTags, err := store.GetPostTags(ctx, s.db, id)
	if err != nil {
		return Post{}, err
	}

	title := duplicateTitle(src.Title)
	slug, err := s.uniqueDuplicateSlug(ctx, src.Slug)
	if err != nil {
		return Post{}, err
	}

	catIDs := categoryIDs(srcCats)
	tagIDs := tagIDs(srcTags)

	var newID string
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		p, err := store.CreatePost(ctx, tx, store.NewPost{
			Title:          title,
			Slug:           slug,
			Excerpt:        src.Excerpt,
			ContentJSON:    src.ContentJSON,
			ContentHTML:    src.ContentHTML,
			ContentText:    src.ContentText,
			CoverImage:     src.CoverImage,
			Status:         store.PostStatusDraft,
			Author:         src.Author,
			Featured:       src.Featured,
			SEOTitle:       src.SEOTitle,
			SEODescription: src.SEODescription,
			CanonicalURL:   src.CanonicalURL,
			OGImage:        src.OGImage,
		})
		if err != nil {
			return err
		}
		newID = p.ID
		if err := store.SetPostCategories(ctx, tx, p.ID, catIDs); err != nil {
			return err
		}
		if err := store.SetPostTags(ctx, tx, p.ID, tagIDs); err != nil {
			return err
		}
		if _, err := store.CreateRevision(ctx, tx, store.NewRevision{
			PostID:      p.ID,
			Title:       title,
			Excerpt:     src.Excerpt,
			ContentJSON: src.ContentJSON,
			ContentHTML: src.ContentHTML,
			EditorID:    currentUserID,
		}); err != nil {
			return err
		}
		_, err = store.PruneRevisions(ctx, tx, p.ID, maxRevisionsPerPost)
		return err
	})
	if err != nil {
		return Post{}, err
	}

	s.logger.Info("post duplicated", slog.String("source_post_id", id), slog.String("post_id", newID))
	return s.loadPost(ctx, newID)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// resolveSlug computes the slug for a CREATE: a provided non-blank value is
// normalized; otherwise it is generated from source (the name/title). Slug
// problems are recorded in fields; the returned error is only a real DB
// failure.
func (s *Service) resolveSlug(ctx context.Context, provided *string, source, excludeID string, fields fieldErrors, exists slugExistsFunc) (string, error) {
	var candidate string
	switch {
	case provided != nil && strings.TrimSpace(*provided) != "":
		candidate = Slugify(*provided)
	case strings.TrimSpace(source) != "":
		candidate = Slugify(source)
	default:
		// No slug supplied and no usable source; the missing name/title is
		// already flagged, so there is nothing to normalize here.
		return "", nil
	}

	if candidate == "" {
		fields.set("slug", slugEmptyMessage)
		return "", nil
	}

	taken, err := exists(ctx, s.db, candidate, excludeID)
	if err != nil {
		return "", err
	}
	if taken {
		fields.set("slug", slugConflictMessage)
		return "", nil
	}
	return candidate, nil
}

// resolveUpdatedSlug computes the slug for an UPDATE: an absent slug keeps
// the existing one; a provided slug is normalized and uniqueness-checked
// (excluding the row itself).
func (s *Service) resolveUpdatedSlug(ctx context.Context, provided *string, existingSlug, excludeID string, fields fieldErrors, exists slugExistsFunc) (string, error) {
	if provided == nil {
		return existingSlug, nil
	}
	candidate := Slugify(*provided)
	if candidate == "" {
		fields.set("slug", slugEmptyMessage)
		return existingSlug, nil
	}
	if candidate == existingSlug {
		return candidate, nil
	}
	taken, err := exists(ctx, s.db, candidate, excludeID)
	if err != nil {
		return "", err
	}
	if taken {
		fields.set("slug", slugConflictMessage)
		return existingSlug, nil
	}
	return candidate, nil
}

// checkAuthor records an "author" field error if authorID is not an existing
// user. A real DB failure is returned as an error.
func (s *Service) checkAuthor(ctx context.Context, authorID string, fields fieldErrors) error {
	_, err := store.GetUserByID(ctx, s.db, authorID)
	if errors.Is(err, store.ErrNotFound) {
		fields.set("author", "Unknown author.")
		return nil
	}
	return err
}

// checkCategories records a single "categories" field error if any id is not
// an existing category.
func (s *Service) checkCategories(ctx context.Context, ids []string, fields fieldErrors) error {
	for _, id := range ids {
		if fields.has("categories") {
			return nil
		}
		_, err := store.GetCategoryByID(ctx, s.db, id)
		if errors.Is(err, store.ErrNotFound) {
			fields.set("categories", "Unknown category.")
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// checkTags records a single "tags" field error if any id is not an existing
// tag.
func (s *Service) checkTags(ctx context.Context, ids []string, fields fieldErrors) error {
	for _, id := range ids {
		if fields.has("tags") {
			return nil
		}
		_, err := store.GetTagByID(ctx, s.db, id)
		if errors.Is(err, store.ErrNotFound) {
			fields.set("tags", "Unknown tag.")
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// checkMedia records a field error (fields[field] = "Unknown media.") if id
// is non-empty and does not reference an existing media row. An empty id
// (unset/cleared cover or OG image) is always valid and short-circuits
// without a lookup.
func (s *Service) checkMedia(ctx context.Context, id, field string, fields fieldErrors) error {
	if id == "" {
		return nil
	}
	_, err := store.GetMediaByID(ctx, s.db, id)
	if errors.Is(err, store.ErrNotFound) {
		fields.set(field, "Unknown media.")
		return nil
	}
	return err
}

// maybeCreateRevision inserts a revision only when the post's
// title/excerpt/content_json/content_html differ from the latest revision (or
// none exists), then prunes to the newest maxRevisionsPerPost.
func (s *Service) maybeCreateRevision(ctx context.Context, tx store.DBTX, postID, title, excerpt, contentJSON, contentHTML, editorID string) error {
	latest, err := store.LatestRevision(ctx, tx, postID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// No revision yet — fall through and create one.
	case err != nil:
		return err
	default:
		if latest.Title == title && latest.Excerpt == excerpt &&
			latest.ContentJSON == contentJSON && latest.ContentHTML == contentHTML {
			return nil
		}
	}

	if _, err := store.CreateRevision(ctx, tx, store.NewRevision{
		PostID:      postID,
		Title:       title,
		Excerpt:     excerpt,
		ContentJSON: contentJSON,
		ContentHTML: contentHTML,
		EditorID:    editorID,
	}); err != nil {
		return err
	}
	_, err = store.PruneRevisions(ctx, tx, postID, maxRevisionsPerPost)
	return err
}

// uniqueDuplicateSlug returns the first free "<base>-copy", "<base>-copy-2",
// "<base>-copy-3"… slug. The base is trimmed so the suffix always fits
// within MaxSlugLen, which also guarantees the loop terminates.
func (s *Service) uniqueDuplicateSlug(ctx context.Context, base string) (string, error) {
	for i := 1; ; i++ {
		suffix := "-copy"
		if i > 1 {
			suffix = "-copy-" + strconv.Itoa(i)
		}
		maxBase := MaxSlugLen - len(suffix)
		if maxBase < 0 {
			maxBase = 0
		}
		trimmedBase := strings.TrimRight(truncateBytes(base, maxBase), "-")
		candidate := trimmedBase + suffix

		taken, err := store.PostSlugExists(ctx, s.db, candidate, "")
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
	}
}

// loadPost fetches a post row and hydrates it with its relations.
func (s *Service) loadPost(ctx context.Context, id string) (Post, error) {
	row, err := store.GetPostByID(ctx, s.db, id)
	if err != nil {
		return Post{}, err
	}
	return s.hydrate(ctx, row, nil)
}

// hydrate loads a post's categories, tags, author and cover/OG media.
// authorCache, when non-nil, memoizes author lookups across a page of
// posts.
func (s *Service) hydrate(ctx context.Context, row store.Post, authorCache map[string]store.User) (Post, error) {
	cats, err := store.GetPostCategories(ctx, s.db, row.ID)
	if err != nil {
		return Post{}, err
	}
	tags, err := store.GetPostTags(ctx, s.db, row.ID)
	if err != nil {
		return Post{}, err
	}

	author, ok := lookupCached(authorCache, row.Author)
	if !ok {
		author, err = store.GetUserByID(ctx, s.db, row.Author)
		if err != nil {
			return Post{}, err
		}
		if authorCache != nil {
			authorCache[row.Author] = author
		}
	}

	coverImage, err := s.lookupMedia(ctx, row.CoverImage)
	if err != nil {
		return Post{}, err
	}
	ogImage, err := s.lookupMedia(ctx, row.OGImage)
	if err != nil {
		return Post{}, err
	}

	return Post{Row: row, Author: author, Categories: cats, Tags: tags, CoverImage: coverImage, OGImage: ogImage}, nil
}

// lookupMedia resolves a possibly-empty media id to its row. An empty id
// returns (nil, nil). A dangling reference (the media row was deleted out
// of band) also returns (nil, nil) rather than an error — see the Post
// struct's CoverImage/OGImage doc comment.
func (s *Service) lookupMedia(ctx context.Context, id string) (*store.Media, error) {
	if id == "" {
		return nil, nil
	}
	m, err := store.GetMediaByID(ctx, s.db, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// inTx runs fn inside a transaction, committing on success and rolling back
// on any error.
func (s *Service) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// validatePostScalars records length/format field errors for the optional
// scalar post fields shared by create and update.
func validatePostScalars(fields fieldErrors, excerpt, seoTitle, seoDesc, canonicalURL string) {
	if runeLen(excerpt) > excerptMax {
		fields.set("excerpt", "Excerpt must be at most 500 characters.")
	}
	if runeLen(seoTitle) > seoTitleMax {
		fields.set("seo_title", "SEO title must be at most 70 characters.")
	}
	if runeLen(seoDesc) > seoDescriptionMax {
		fields.set("seo_description", "SEO description must be at most 170 characters.")
	}
	if canonicalURL != "" && !isHTTPURL(canonicalURL) {
		fields.set("canonical_url", "Canonical URL must be a valid http(s) URL.")
	}
}

// resolveContent validates and normalizes the incoming content_json /
// content_html pair for create and update:
//
//   - content_json is stored verbatim once it parses as valid JSON; a blank
//     value becomes "{}". A value over contentJSONMax or one that is not valid
//     JSON records a "content_json" field error.
//   - content_html is sanitized against the allowlist (see Sanitize), and the
//     plain-text body is derived from the sanitized HTML (see HTMLToText). A
//     value over contentHTMLMax records a "content_html" field error and is
//     not processed further.
//
// The returned values are only meaningful when no content field error was
// recorded (the caller checks fields before using them).
func resolveContent(rawJSON, rawHTML string, fields fieldErrors) (contentJSON, contentHTML, contentText string) {
	switch {
	case len(rawJSON) > contentJSONMax:
		fields.set("content_json", contentJSONTooLargeMessage)
	case strings.TrimSpace(rawJSON) == "":
		contentJSON = emptyContentJSON
	case !json.Valid([]byte(rawJSON)):
		fields.set("content_json", contentJSONInvalidMessage)
	default:
		contentJSON = rawJSON
	}

	if len(rawHTML) > contentHTMLMax {
		fields.set("content_html", contentHTMLTooLargeMessage)
	} else {
		contentHTML = Sanitize(rawHTML)
		contentText = HTMLToText(contentHTML)
	}
	return contentJSON, contentHTML, contentText
}

// resolveWritableStatus validates a requested status against the Phase 3
// rules: draft and archived pass through; published/scheduled are rejected
// with the frozen "later phase" message; anything else is invalid. It
// returns draft as a safe placeholder when it records an error.
func resolveWritableStatus(status string, fields fieldErrors) string {
	switch status {
	case store.PostStatusDraft, store.PostStatusArchived:
		return status
	case store.PostStatusPublished, store.PostStatusScheduled:
		fields.set("status", statusPublishMessage)
	default:
		fields.set("status", statusInvalidMessage)
	}
	return store.PostStatusDraft
}

// duplicateTitle builds the "Copy of <title>" title, capped at titleMax
// characters.
func duplicateTitle(orig string) string {
	title := "Copy of " + orig
	if runeLen(title) > titleMax {
		title = truncateRunes(title, titleMax)
	}
	return title
}

func categoryIDs(cats []store.Category) []string {
	ids := make([]string, 0, len(cats))
	for _, c := range cats {
		ids = append(ids, c.ID)
	}
	return ids
}

func tagIDs(tags []store.Tag) []string {
	ids := make([]string, 0, len(tags))
	for _, t := range tags {
		ids = append(ids, t.ID)
	}
	return ids
}

func lookupCached(cache map[string]store.User, id string) (store.User, bool) {
	if cache == nil {
		return store.User{}, false
	}
	u, ok := cache[id]
	return u, ok
}

// runeLen counts characters (runes), the unit the plan's length limits use.
func runeLen(s string) int {
	return utf8.RuneCountInString(s)
}

// truncateRunes returns the first n runes of s.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n])
}

// truncateBytes returns the first n bytes of s (safe for ASCII slugs only).
func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// isHTTPURL reports whether raw parses as an absolute http(s) URL with a
// host.
func isHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
