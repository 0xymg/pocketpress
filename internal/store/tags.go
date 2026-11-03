package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Tag is a row of the tags table. Unlike a category, a tag has no
// description.
type Tag struct {
	ID        string
	Name      string
	Slug      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewTag holds the fields a caller supplies to CreateTag. ID and timestamps
// are set by CreateTag itself; Slug must already be normalized.
type NewTag struct {
	Name string
	Slug string
}

// CreateTag inserts a new tag and returns it as stored. A slug collision
// surfaces as the underlying tags.slug UNIQUE constraint error; the content
// service pre-checks uniqueness with TagSlugExists.
func CreateTag(ctx context.Context, db DBTX, in NewTag) (Tag, error) {
	id, err := newID()
	if err != nil {
		return Tag{}, err
	}

	now := time.Now().UTC()
	tg := Tag{
		ID:        id,
		Name:      in.Name,
		Slug:      in.Slug,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO tags (id, name, slug, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`,
		tg.ID, tg.Name, tg.Slug, formatTime(tg.CreatedAt), formatTime(tg.UpdatedAt),
	)
	if err != nil {
		return Tag{}, fmt.Errorf("insert tag: %w", err)
	}
	return tg, nil
}

// GetTagByID looks up a tag by ID. Returns ErrNotFound if no such tag
// exists.
func GetTagByID(ctx context.Context, db DBTX, id string) (Tag, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, name, slug, created_at, updated_at
		FROM tags WHERE id = ?`, id)
	return scanTag(row)
}

// GetTagBySlug looks up a tag by slug. Returns ErrNotFound if no such tag
// exists.
func GetTagBySlug(ctx context.Context, db DBTX, slug string) (Tag, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, name, slug, created_at, updated_at
		FROM tags WHERE slug = ?`, slug)
	return scanTag(row)
}

// ListTags returns every tag ordered by name (case-insensitive).
func ListTags(ctx context.Context, db DBTX) ([]Tag, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, slug, created_at, updated_at
		FROM tags ORDER BY name COLLATE NOCASE ASC`)
	if err != nil {
		return nil, fmt.Errorf("query tags: %w", err)
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
		return nil, fmt.Errorf("iterate tags: %w", err)
	}
	return tags, nil
}

// UpdateTag overwrites a tag's name and slug (and bumps updated_at). The
// caller supplies the already-resolved final values. Returns ErrNotFound if
// no such tag exists.
func UpdateTag(ctx context.Context, db DBTX, id, name, slug string) error {
	res, err := db.ExecContext(ctx, `
		UPDATE tags SET name = ?, slug = ?, updated_at = ? WHERE id = ?`,
		name, slug, formatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("update tag: %w", err)
	}
	return requireRowsAffected(res)
}

// DeleteTag removes a tag. Its post_tags join rows cascade away; the posts
// themselves remain. Returns ErrNotFound if no such tag exists.
func DeleteTag(ctx context.Context, db DBTX, id string) error {
	res, err := db.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}
	return requireRowsAffected(res)
}

// TagSlugExists reports whether a tag other than excludeID already uses
// slug. Pass excludeID = "" on create.
func TagSlugExists(ctx context.Context, db DBTX, slug, excludeID string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM tags WHERE slug = ? AND id <> ?)`,
		slug, excludeID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check tag slug exists: %w", err)
	}
	return exists, nil
}

func scanTag(row rowScanner) (Tag, error) {
	var (
		tg        Tag
		createdAt string
		updatedAt string
	)
	err := row.Scan(&tg.ID, &tg.Name, &tg.Slug, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Tag{}, ErrNotFound
	}
	if err != nil {
		return Tag{}, fmt.Errorf("scan tag: %w", err)
	}

	if tg.CreatedAt, err = parseTime(createdAt); err != nil {
		return Tag{}, err
	}
	if tg.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Tag{}, err
	}
	return tg, nil
}
