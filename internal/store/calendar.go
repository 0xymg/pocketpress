package store

import (
	"context"
	"fmt"
	"time"
)

// MaxCalendarPosts caps a single calendar range query so a wide window
// cannot pull the entire table into memory.
const MaxCalendarPosts = 500

// ListCalendarPosts returns scheduled and published posts that fall inside
// the inclusive [from, to] window:
//
//   - scheduled posts whose scheduled_for is in range
//   - published posts whose published_at is in range
//
// Ordered by the placement timestamp ascending (scheduled_for for
// scheduled, published_at for published), then id. Capped at
// MaxCalendarPosts.
func ListCalendarPosts(ctx context.Context, db DBTX, from, to time.Time) ([]Post, error) {
	from = from.UTC()
	to = to.UTC()
	rows, err := db.QueryContext(ctx, `
		SELECT id, title, slug, excerpt, content_json, content_html, content_text,
			cover_image, status, author, featured, seo_title, seo_description,
			canonical_url, og_image, locale, translation_group,
			scheduled_for, published_at,
			review_status, review_note, reviewed_by, reviewed_at, submitted_for_review_at,
			created_at, updated_at
		FROM posts
		WHERE (
			(status = ? AND scheduled_for IS NOT NULL
				AND scheduled_for >= ? AND scheduled_for <= ?)
			OR
			(status = ? AND published_at IS NOT NULL
				AND published_at >= ? AND published_at <= ?)
		)
		ORDER BY COALESCE(scheduled_for, published_at) ASC, id ASC
		LIMIT ?`,
		PostStatusScheduled, formatTime(from), formatTime(to),
		PostStatusPublished, formatTime(from), formatTime(to),
		MaxCalendarPosts,
	)
	if err != nil {
		return nil, fmt.Errorf("query calendar posts: %w", err)
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
		return nil, fmt.Errorf("iterate calendar posts: %w", err)
	}
	return posts, nil
}
