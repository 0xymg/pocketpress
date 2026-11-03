package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PostRevision is a row of the post_revisions table: an immutable snapshot
// of a post's title/excerpt/content_json/content_html taken when the post is
// saved with changes.
type PostRevision struct {
	ID          string
	PostID      string
	Title       string
	Excerpt     string
	ContentJSON string
	ContentHTML string
	EditorID    string
	CreatedAt   time.Time
}

// NewRevision holds the fields a caller supplies to CreateRevision. ID and
// CreatedAt are set by CreateRevision; a blank ContentJSON is normalized to
// the '{}' placeholder.
type NewRevision struct {
	PostID      string
	Title       string
	Excerpt     string
	ContentJSON string
	ContentHTML string
	EditorID    string
}

// CreateRevision inserts a revision snapshot and returns it as stored.
func CreateRevision(ctx context.Context, db DBTX, in NewRevision) (PostRevision, error) {
	id, err := newID()
	if err != nil {
		return PostRevision{}, err
	}

	rev := PostRevision{
		ID:          id,
		PostID:      in.PostID,
		Title:       in.Title,
		Excerpt:     in.Excerpt,
		ContentJSON: contentJSONOrDefault(in.ContentJSON),
		ContentHTML: in.ContentHTML,
		EditorID:    in.EditorID,
		CreatedAt:   time.Now().UTC(),
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO post_revisions (id, post_id, title, excerpt, content_json, content_html, editor_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rev.ID, rev.PostID, rev.Title, nullString(rev.Excerpt), rev.ContentJSON, rev.ContentHTML,
		rev.EditorID, formatTime(rev.CreatedAt),
	)
	if err != nil {
		return PostRevision{}, fmt.Errorf("insert post revision: %w", err)
	}
	return rev, nil
}

// LatestRevision returns a post's most recent revision. Ordering is by
// created_at then id, both descending; because ids are monotonic UUIDv7
// values the id tiebreaker resolves revisions created within the same
// (second-granularity) timestamp in true insertion order. Returns
// ErrNotFound if the post has no revisions.
func LatestRevision(ctx context.Context, db DBTX, postID string) (PostRevision, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, post_id, title, excerpt, content_json, content_html, editor_id, created_at
		FROM post_revisions
		WHERE post_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, postID)
	return scanRevision(row)
}

// CountRevisions returns how many revisions a post has.
func CountRevisions(ctx context.Context, db DBTX, postID string) (int, error) {
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM post_revisions WHERE post_id = ?`, postID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count post revisions: %w", err)
	}
	return count, nil
}

// PruneRevisions keeps only the newest keep revisions for a post, deleting
// the rest. Newness is (created_at, id) descending, matching
// LatestRevision. It returns the number of revisions removed.
func PruneRevisions(ctx context.Context, db DBTX, postID string, keep int) (int64, error) {
	res, err := db.ExecContext(ctx, `
		DELETE FROM post_revisions
		WHERE post_id = ?
		  AND id NOT IN (
			SELECT id FROM post_revisions
			WHERE post_id = ?
			ORDER BY created_at DESC, id DESC
			LIMIT ?
		  )`, postID, postID, keep)
	if err != nil {
		return 0, fmt.Errorf("prune post revisions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read rows affected: %w", err)
	}
	return n, nil
}

func scanRevision(row rowScanner) (PostRevision, error) {
	var (
		rev       PostRevision
		excerpt   sql.NullString
		createdAt string
	)
	err := row.Scan(&rev.ID, &rev.PostID, &rev.Title, &excerpt, &rev.ContentJSON, &rev.ContentHTML, &rev.EditorID, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PostRevision{}, ErrNotFound
	}
	if err != nil {
		return PostRevision{}, fmt.Errorf("scan post revision: %w", err)
	}

	rev.Excerpt = stringOrEmpty(excerpt)
	if rev.CreatedAt, err = parseTime(createdAt); err != nil {
		return PostRevision{}, err
	}
	return rev, nil
}
