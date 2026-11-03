package content

import (
	"context"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

// PublicPostListQuery is the resolved (already clamped/normalized by the
// handler) query for ListPublicPosts. CategoryID/TagID are resolved from
// the request's category/tag slugs by the caller (empty = no filter);
// Featured nil means "any"; AuthorID is a user id ("" = any).
type PublicPostListQuery struct {
	Page       int
	PerPage    int
	CategoryID string
	TagID      string
	Featured   *bool
	AuthorID   string
	Sort       store.PostSortField
	SortDesc   bool
}

// ListPublicPosts returns one page of publicly-visible posts — status
// PostStatusPublished, published_at set and not in the future — hydrated
// with their author, categories, tags and cover/OG media. now is pinned
// once per call so the count and the page of rows it returns agree even as
// time advances during the request.
//
// This is the public API's own query path: it never accepts a Status
// filter from the caller (unlike the admin ListPosts), so a public request
// can never surface a draft, scheduled or archived post.
func (s *Service) ListPublicPosts(ctx context.Context, q PublicPostListQuery) (PostListResult, error) {
	now := time.Now().UTC()
	filter := store.PostFilter{
		Status:          store.PostStatusPublished,
		PublishedBefore: &now,
		CategoryID:      q.CategoryID,
		TagID:           q.TagID,
		Featured:        q.Featured,
		AuthorID:        q.AuthorID,
	}

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

// GetPublicPostBySlug returns a single publicly-visible post by slug. It
// returns store.ErrNotFound both when no post has that slug at all, and —
// the "hard" visibility rule from the development plan's public API
// contract — for a draft, archived, or scheduled-but-not-yet-published
// post, so callers cannot distinguish "doesn't exist" from "not public
// yet".
func (s *Service) GetPublicPostBySlug(ctx context.Context, slug string) (Post, error) {
	row, err := store.GetPostBySlug(ctx, s.db, slug)
	if err != nil {
		return Post{}, err
	}
	if !isPubliclyVisible(row, time.Now().UTC()) {
		return Post{}, store.ErrNotFound
	}
	return s.hydrate(ctx, row, nil)
}

// isPubliclyVisible implements the public API's hard visibility rule:
// status must be published, published_at must be set, and published_at
// must not be in the future.
func isPubliclyVisible(p store.Post, now time.Time) bool {
	return p.Status == store.PostStatusPublished && p.PublishedAt != nil && !p.PublishedAt.After(now)
}

// GetCategoryBySlug and GetTagBySlug resolve a slug to a category/tag, for
// the public API's category/tag detail and category/tag-scoped post-list
// endpoints. There is no visibility rule for categories or tags themselves
// — only posts carry draft/scheduled/archived states.
func (s *Service) GetCategoryBySlug(ctx context.Context, slug string) (store.Category, error) {
	return store.GetCategoryBySlug(ctx, s.db, slug)
}

func (s *Service) GetTagBySlug(ctx context.Context, slug string) (store.Tag, error) {
	return store.GetTagBySlug(ctx, s.db, slug)
}
