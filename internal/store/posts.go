package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Post statuses. The full set is constrained by the posts.status CHECK in
// the 0001_init migration; Phase 3 only ever writes draft and archived (the
// publishing states arrive in a later phase).
const (
	PostStatusDraft     = "draft"
	PostStatusScheduled = "scheduled"
	PostStatusPublished = "published"
	PostStatusArchived  = "archived"
)

// emptyContentJSON is the placeholder stored in posts.content_json /
// post_revisions.content_json when a caller supplies no serialized editor
// state (a title-only draft). The column is NOT NULL, so an empty input is
// normalized to the JSON empty object '{}'.
const emptyContentJSON = "{}"

// contentJSONOrDefault normalizes a caller-supplied content_json: a blank
// value becomes the '{}' placeholder so the NOT NULL column always holds
// valid JSON.
func contentJSONOrDefault(s string) string {
	if s == "" {
		return emptyContentJSON
	}
	return s
}

// Post is a row of the posts table (scalar columns only; the related
// categories, tags and author are loaded separately).
type Post struct {
	ID             string
	Title          string
	Slug           string
	Excerpt        string
	ContentJSON    string
	ContentHTML    string
	ContentText    string
	CoverImage     string
	Status         string
	Author         string // user id
	Featured       bool
	SEOTitle       string
	SEODescription string
	CanonicalURL   string
	OGImage        string
	ScheduledFor   *time.Time
	PublishedAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewPost holds the writable fields for CreatePost. ID and timestamps are set
// by CreatePost itself. CoverImage/OGImage hold a media.id ("" means unset,
// the Phase 4 media service validates the id before this is called).
// scheduled_for and published_at are left NULL — the publishing phase
// populates them.
type NewPost struct {
	Title          string
	Slug           string
	Excerpt        string
	ContentJSON    string
	ContentHTML    string
	ContentText    string
	CoverImage     string
	Status         string
	Author         string
	Featured       bool
	SEOTitle       string
	SEODescription string
	CanonicalURL   string
	OGImage        string
}

// PostUpdate holds the resolved writable fields for UpdatePost, including
// CoverImage/OGImage (a media.id, or "" to clear). It deliberately does not
// touch published_at or scheduled_for, so UpdatePost omits those columns
// entirely.
type PostUpdate struct {
	Title          string
	Slug           string
	Excerpt        string
	ContentJSON    string
	ContentHTML    string
	ContentText    string
	CoverImage     string
	Status         string
	Author         string
	Featured       bool
	SEOTitle       string
	SEODescription string
	CanonicalURL   string
	OGImage        string
}

// PostFilter is the set of list filters ListPosts and CountPosts share.
type PostFilter struct {
	Status     string // "" = any
	CategoryID string // "" = any
	TagID      string // "" = any
	AuthorID   string // "" = any
	Featured   *bool  // nil = any
	Search     string // "" = any; FTS5 over title, excerpt, content_text

	// PublishedBefore, when non-nil, restricts results to rows with a
	// non-NULL published_at at or before this time — the public API's
	// visibility rule (combined with Status = PostStatusPublished). nil
	// applies no constraint, so admin listings are unaffected.
	PublishedBefore *time.Time
}

// PostSortField selects the column ListPosts orders by. The zero value
// (PostSortUpdatedAt) is the admin list's long-standing default.
type PostSortField int

const (
	PostSortUpdatedAt PostSortField = iota
	PostSortPublishedAt
	PostSortTitle
)

// CreatePost inserts a new post and returns it as stored.
func CreatePost(ctx context.Context, db DBTX, in NewPost) (Post, error) {
	id, err := newID()
	if err != nil {
		return Post{}, err
	}

	now := time.Now().UTC()
	p := Post{
		ID:             id,
		Title:          in.Title,
		Slug:           in.Slug,
		Excerpt:        in.Excerpt,
		ContentJSON:    contentJSONOrDefault(in.ContentJSON),
		ContentHTML:    in.ContentHTML,
		ContentText:    in.ContentText,
		CoverImage:     in.CoverImage,
		Status:         in.Status,
		Author:         in.Author,
		Featured:       in.Featured,
		SEOTitle:       in.SEOTitle,
		SEODescription: in.SEODescription,
		CanonicalURL:   in.CanonicalURL,
		OGImage:        in.OGImage,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO posts (
			id, title, slug, excerpt, content_json, content_html, content_text,
			cover_image, status, author, featured, seo_title, seo_description,
			canonical_url, og_image, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Title, p.Slug, nullString(p.Excerpt), p.ContentJSON, p.ContentHTML, p.ContentText,
		nullString(p.CoverImage), p.Status, p.Author, boolToInt(p.Featured), nullString(p.SEOTitle),
		nullString(p.SEODescription), nullString(p.CanonicalURL), nullString(p.OGImage),
		formatTime(p.CreatedAt), formatTime(p.UpdatedAt),
	)
	if err != nil {
		return Post{}, fmt.Errorf("insert post: %w", err)
	}
	return p, nil
}

// UpdatePost overwrites a post's editable Phase 3 columns (and bumps
// updated_at), leaving publishing/media columns untouched. Returns
// ErrNotFound if no such post exists.
func UpdatePost(ctx context.Context, db DBTX, id string, in PostUpdate) error {
	res, err := db.ExecContext(ctx, `
		UPDATE posts SET
			title = ?, slug = ?, excerpt = ?, content_json = ?, content_html = ?, content_text = ?,
			cover_image = ?, status = ?, author = ?, featured = ?, seo_title = ?, seo_description = ?,
			canonical_url = ?, og_image = ?, updated_at = ?
		WHERE id = ?`,
		in.Title, in.Slug, nullString(in.Excerpt), contentJSONOrDefault(in.ContentJSON), in.ContentHTML, in.ContentText,
		nullString(in.CoverImage), in.Status, in.Author, boolToInt(in.Featured), nullString(in.SEOTitle),
		nullString(in.SEODescription), nullString(in.CanonicalURL), nullString(in.OGImage),
		formatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("update post: %w", err)
	}
	return requireRowsAffected(res)
}

// SetPostStatus overwrites a post's status, published_at and scheduled_for
// columns (and bumps updated_at), leaving every other column untouched. It
// is the write path for the publishing operations and the scheduled-publish
// worker — the general UpdatePost deliberately never touches these three
// columns, so status transitions and content edits stay independent. A nil
// publishedAt/scheduledFor stores SQL NULL. Returns ErrNotFound if no such
// post exists.
func SetPostStatus(ctx context.Context, db DBTX, id, status string, publishedAt, scheduledFor *time.Time) error {
	res, err := db.ExecContext(ctx, `
		UPDATE posts SET status = ?, published_at = ?, scheduled_for = ?, updated_at = ?
		WHERE id = ?`,
		status, nullTime(publishedAt), nullTime(scheduledFor), formatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("set post status: %w", err)
	}
	return requireRowsAffected(res)
}

// ListDuePosts returns every scheduled post whose scheduled_for is at or
// before now, ordered by scheduled_for (oldest first). It is the query
// behind the scheduled-publish worker's promotion pass.
func ListDuePosts(ctx context.Context, db DBTX, now time.Time) ([]Post, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, title, slug, excerpt, content_json, content_html, content_text,
			cover_image, status, author, featured, seo_title, seo_description,
			canonical_url, og_image, scheduled_for, published_at, created_at, updated_at
		FROM posts
		WHERE status = ? AND scheduled_for IS NOT NULL AND scheduled_for <= ?
		ORDER BY scheduled_for ASC, id ASC`,
		PostStatusScheduled, formatTime(now),
	)
	if err != nil {
		return nil, fmt.Errorf("query due posts: %w", err)
	}
	defer rows.Close()

	posts := make([]Post, 0)
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due posts: %w", err)
	}
	return posts, nil
}

// DeletePost removes a post. Its revisions and category/tag join rows
// cascade away (ON DELETE CASCADE). Returns ErrNotFound if no such post
// exists.
func DeletePost(ctx context.Context, db DBTX, id string) error {
	res, err := db.ExecContext(ctx, `DELETE FROM posts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete post: %w", err)
	}
	return requireRowsAffected(res)
}

// GetPostByID looks up a post by ID. Returns ErrNotFound if no such post
// exists.
func GetPostByID(ctx context.Context, db DBTX, id string) (Post, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, title, slug, excerpt, content_json, content_html, content_text,
			cover_image, status, author, featured, seo_title, seo_description,
			canonical_url, og_image, scheduled_for, published_at, created_at, updated_at
		FROM posts WHERE id = ?`, id)
	return scanPost(row)
}

// GetPostBySlug looks up a post by slug, regardless of status — callers
// that must only surface publicly-visible posts (the public API) apply
// that check themselves after loading the row. Returns ErrNotFound if no
// such post exists.
func GetPostBySlug(ctx context.Context, db DBTX, slug string) (Post, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, title, slug, excerpt, content_json, content_html, content_text,
			cover_image, status, author, featured, seo_title, seo_description,
			canonical_url, og_image, scheduled_for, published_at, created_at, updated_at
		FROM posts WHERE slug = ?`, slug)
	return scanPost(row)
}

// PostSlugExists reports whether a post other than excludeID already uses
// slug. Pass excludeID = "" on create.
func PostSlugExists(ctx context.Context, db DBTX, slug, excludeID string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM posts WHERE slug = ? AND id <> ?)`,
		slug, excludeID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check post slug exists: %w", err)
	}
	return exists, nil
}

// ListPosts returns one page of posts matching filter, ordered by sort
// (sortDesc = descending). limit/offset paginate. The id tiebreaker keeps
// ordering stable when several posts share a sort value.
//
// When filter.Search is non-empty, results are restricted via the posts_fts
// FTS5 index (title, excerpt, content_text) and ordered primarily by
// BM25 relevance (title weighted highest), then by the requested sort.
func ListPosts(ctx context.Context, db DBTX, filter PostFilter, sort PostSortField, sortDesc bool, limit, offset int) ([]Post, error) {
	from, where, args, useFTS := postQueryParts(filter)
	direction := "ASC"
	if sortDesc {
		direction = "DESC"
	}

	orderBy := sortColumn(sort, useFTS)
	if useFTS {
		// bm25 weights: title=10, excerpt=5, content_text=1. Lower is better.
		orderBy = `bm25(posts_fts, 10.0, 5.0, 1.0) ASC, ` + orderBy + ` ` + direction + `, posts.id ` + direction
	} else {
		orderBy = orderBy + ` ` + direction + `, id ` + direction
	}

	cols := postSelectColumns(useFTS)
	query := `SELECT ` + cols + ` FROM ` + from + where +
		` ORDER BY ` + orderBy +
		` LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query posts: %w", err)
	}
	defer rows.Close()

	posts := make([]Post, 0)
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate posts: %w", err)
	}
	return posts, nil
}

// CountPosts returns how many posts match filter (ignoring pagination), for
// list response metadata.
func CountPosts(ctx context.Context, db DBTX, filter PostFilter) (int, error) {
	from, where, args, _ := postQueryParts(filter)
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+from+where, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count posts: %w", err)
	}
	return count, nil
}

// postQueryParts builds the FROM/WHERE fragments (and args) shared by
// ListPosts and CountPosts. useFTS is true when the query joins posts_fts.
func postQueryParts(filter PostFilter) (from, where string, args []any, useFTS bool) {
	from = "posts"
	var conds []string

	if filter.Status != "" {
		conds = append(conds, "posts.status = ?")
		args = append(args, filter.Status)
	}
	if filter.CategoryID != "" {
		conds = append(conds, "EXISTS (SELECT 1 FROM post_categories pc WHERE pc.post_id = posts.id AND pc.category_id = ?)")
		args = append(args, filter.CategoryID)
	}
	if filter.TagID != "" {
		conds = append(conds, "EXISTS (SELECT 1 FROM post_tags pt WHERE pt.post_id = posts.id AND pt.tag_id = ?)")
		args = append(args, filter.TagID)
	}
	if filter.AuthorID != "" {
		conds = append(conds, "posts.author = ?")
		args = append(args, filter.AuthorID)
	}
	if filter.Featured != nil {
		conds = append(conds, "posts.featured = ?")
		args = append(args, boolToInt(*filter.Featured))
	}
	if filter.Search != "" {
		ftsExpr, ok := BuildFTSQuery(filter.Search)
		if !ok {
			// Punctuation-only / empty-after-sanitize: match nothing.
			conds = append(conds, "1 = 0")
		} else {
			useFTS = true
			from = "posts JOIN posts_fts ON posts.rowid = posts_fts.rowid"
			conds = append(conds, "posts_fts MATCH ?")
			args = append(args, ftsExpr)
		}
	}
	if filter.PublishedBefore != nil {
		conds = append(conds, "posts.published_at IS NOT NULL AND posts.published_at <= ?")
		args = append(args, formatTime(*filter.PublishedBefore))
	}

	if len(conds) == 0 {
		return from, "", args, useFTS
	}
	return from, " WHERE " + strings.Join(conds, " AND "), args, useFTS
}

// postSelectColumns returns the SELECT list for ListPosts. When useFTS is
// true, columns are qualified with posts. so they do not collide with the
// identically named FTS columns.
func postSelectColumns(useFTS bool) string {
	cols := `id, title, slug, excerpt, content_json, content_html, content_text,
			cover_image, status, author, featured, seo_title, seo_description,
			canonical_url, og_image, scheduled_for, published_at, created_at, updated_at`
	if !useFTS {
		return cols
	}
	return `posts.id, posts.title, posts.slug, posts.excerpt, posts.content_json, posts.content_html, posts.content_text,
			posts.cover_image, posts.status, posts.author, posts.featured, posts.seo_title, posts.seo_description,
			posts.canonical_url, posts.og_image, posts.scheduled_for, posts.published_at, posts.created_at, posts.updated_at`
}

// sortColumn maps a PostSortField to its ORDER BY column expression. Title
// sorts case-insensitively (COLLATE NOCASE), matching how categories and
// tags are already ordered by name elsewhere in this package; updated_at
// and published_at are RFC3339 UTC TEXT columns, which sort correctly
// lexicographically. qualify prefixes posts. when joining FTS.
func sortColumn(sort PostSortField, qualify bool) string {
	prefix := ""
	if qualify {
		prefix = "posts."
	}
	switch sort {
	case PostSortPublishedAt:
		return prefix + "published_at"
	case PostSortTitle:
		return prefix + "title COLLATE NOCASE"
	default:
		return prefix + "updated_at"
	}
}

// SetPostCategories replaces a post's category associations with exactly the
// given ids (deduplicated). Passing an empty slice clears them.
func SetPostCategories(ctx context.Context, db DBTX, postID string, categoryIDs []string) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM post_categories WHERE post_id = ?`, postID); err != nil {
		return fmt.Errorf("clear post categories: %w", err)
	}
	for _, catID := range dedupe(categoryIDs) {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO post_categories (post_id, category_id) VALUES (?, ?)`, postID, catID,
		); err != nil {
			return fmt.Errorf("insert post category: %w", err)
		}
	}
	return nil
}

// SetPostTags replaces a post's tag associations with exactly the given ids
// (deduplicated). Passing an empty slice clears them.
func SetPostTags(ctx context.Context, db DBTX, postID string, tagIDs []string) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM post_tags WHERE post_id = ?`, postID); err != nil {
		return fmt.Errorf("clear post tags: %w", err)
	}
	for _, tagID := range dedupe(tagIDs) {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO post_tags (post_id, tag_id) VALUES (?, ?)`, postID, tagID,
		); err != nil {
			return fmt.Errorf("insert post tag: %w", err)
		}
	}
	return nil
}

// GetPostCategories returns a post's categories, ordered by name.
func GetPostCategories(ctx context.Context, db DBTX, postID string) ([]Category, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.id, c.name, c.slug, c.description, c.created_at, c.updated_at
		FROM categories c
		JOIN post_categories pc ON pc.category_id = c.id
		WHERE pc.post_id = ?
		ORDER BY c.name COLLATE NOCASE ASC`, postID)
	if err != nil {
		return nil, fmt.Errorf("query post categories: %w", err)
	}
	defer rows.Close()

	categories := make([]Category, 0)
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		categories = append(categories, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate post categories: %w", err)
	}
	return categories, nil
}

// GetPostTags returns a post's tags, ordered by name.
func GetPostTags(ctx context.Context, db DBTX, postID string) ([]Tag, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT t.id, t.name, t.slug, t.created_at, t.updated_at
		FROM tags t
		JOIN post_tags pt ON pt.tag_id = t.id
		WHERE pt.post_id = ?
		ORDER BY t.name COLLATE NOCASE ASC`, postID)
	if err != nil {
		return nil, fmt.Errorf("query post tags: %w", err)
	}
	defer rows.Close()

	tags := make([]Tag, 0)
	for rows.Next() {
		tg, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		tags = append(tags, tg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate post tags: %w", err)
	}
	return tags, nil
}

func scanPost(row rowScanner) (Post, error) {
	var (
		p            Post
		excerpt      sql.NullString
		coverImage   sql.NullString
		featured     int
		seoTitle     sql.NullString
		seoDesc      sql.NullString
		canonicalURL sql.NullString
		ogImage      sql.NullString
		scheduledFor sql.NullString
		publishedAt  sql.NullString
		createdAt    string
		updatedAt    string
	)

	err := row.Scan(
		&p.ID, &p.Title, &p.Slug, &excerpt, &p.ContentJSON, &p.ContentHTML, &p.ContentText,
		&coverImage, &p.Status, &p.Author, &featured, &seoTitle, &seoDesc,
		&canonicalURL, &ogImage, &scheduledFor, &publishedAt, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Post{}, ErrNotFound
	}
	if err != nil {
		return Post{}, fmt.Errorf("scan post: %w", err)
	}

	p.Excerpt = stringOrEmpty(excerpt)
	p.CoverImage = stringOrEmpty(coverImage)
	p.Featured = featured != 0
	p.SEOTitle = stringOrEmpty(seoTitle)
	p.SEODescription = stringOrEmpty(seoDesc)
	p.CanonicalURL = stringOrEmpty(canonicalURL)
	p.OGImage = stringOrEmpty(ogImage)

	if p.CreatedAt, err = parseTime(createdAt); err != nil {
		return Post{}, err
	}
	if p.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Post{}, err
	}
	if p.ScheduledFor, err = parseTimePtr(scheduledFor); err != nil {
		return Post{}, err
	}
	if p.PublishedAt, err = parseTimePtr(publishedAt); err != nil {
		return Post{}, err
	}
	return p, nil
}

// parseTimePtr converts a nullable stored timestamp to *time.Time (nil when
// NULL), the read side of the optional scheduled_for/published_at columns.
func parseTimePtr(ns sql.NullString) (*time.Time, error) {
	if !ns.Valid {
		return nil, nil
	}
	t, err := parseTime(ns.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// dedupe returns ids with duplicates removed, preserving first-seen order.
func dedupe(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
