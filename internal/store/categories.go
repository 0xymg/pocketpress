package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Category is a row of the categories table.
type Category struct {
	ID          string
	Name        string
	Slug        string
	Description string // "" means unset (NULL in storage)
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NewCategory holds the fields a caller supplies to CreateCategory. ID and
// timestamps are set by CreateCategory itself. Slug must already be
// normalized by the caller (the content service owns slug rules).
type NewCategory struct {
	Name        string
	Slug        string
	Description string
}

// CreateCategory inserts a new category and returns it as stored. A slug
// collision surfaces as the underlying categories.slug UNIQUE constraint
// error; the content service pre-checks uniqueness with CategorySlugExists
// so it can return a field-level validation error instead.
func CreateCategory(ctx context.Context, db DBTX, in NewCategory) (Category, error) {
	id, err := newID()
	if err != nil {
		return Category{}, err
	}

	now := time.Now().UTC()
	c := Category{
		ID:          id,
		Name:        in.Name,
		Slug:        in.Slug,
		Description: in.Description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO categories (id, name, slug, description, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Slug, nullString(c.Description), formatTime(c.CreatedAt), formatTime(c.UpdatedAt),
	)
	if err != nil {
		return Category{}, fmt.Errorf("insert category: %w", err)
	}
	return c, nil
}

// GetCategoryByID looks up a category by ID. Returns ErrNotFound if no such
// category exists.
func GetCategoryByID(ctx context.Context, db DBTX, id string) (Category, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, name, slug, description, created_at, updated_at
		FROM categories WHERE id = ?`, id)
	return scanCategory(row)
}

// GetCategoryBySlug looks up a category by slug. Returns ErrNotFound if no
// such category exists.
func GetCategoryBySlug(ctx context.Context, db DBTX, slug string) (Category, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, name, slug, description, created_at, updated_at
		FROM categories WHERE slug = ?`, slug)
	return scanCategory(row)
}

// ListCategories returns every category ordered by name (case-insensitive).
func ListCategories(ctx context.Context, db DBTX) ([]Category, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, slug, description, created_at, updated_at
		FROM categories ORDER BY name COLLATE NOCASE ASC`)
	if err != nil {
		return nil, fmt.Errorf("query categories: %w", err)
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
		return nil, fmt.Errorf("iterate categories: %w", err)
	}
	return categories, nil
}

// UpdateCategory overwrites a category's name, slug and description (and
// bumps updated_at). The caller supplies the already-resolved final values.
// Returns ErrNotFound if no such category exists.
func UpdateCategory(ctx context.Context, db DBTX, id, name, slug, description string) error {
	res, err := db.ExecContext(ctx, `
		UPDATE categories SET name = ?, slug = ?, description = ?, updated_at = ?
		WHERE id = ?`,
		name, slug, nullString(description), formatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("update category: %w", err)
	}
	return requireRowsAffected(res)
}

// DeleteCategory removes a category. Its post_categories join rows cascade
// away (ON DELETE CASCADE in the schema); the posts themselves remain.
// Returns ErrNotFound if no such category exists.
func DeleteCategory(ctx context.Context, db DBTX, id string) error {
	res, err := db.ExecContext(ctx, `DELETE FROM categories WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete category: %w", err)
	}
	return requireRowsAffected(res)
}

// CategorySlugExists reports whether a category other than excludeID already
// uses slug. Pass excludeID = "" on create; pass the category's own id on
// update so renaming a category to its current slug is not a conflict.
func CategorySlugExists(ctx context.Context, db DBTX, slug, excludeID string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM categories WHERE slug = ? AND id <> ?)`,
		slug, excludeID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check category slug exists: %w", err)
	}
	return exists, nil
}

func scanCategory(row rowScanner) (Category, error) {
	var (
		c           Category
		description sql.NullString
		createdAt   string
		updatedAt   string
	)
	err := row.Scan(&c.ID, &c.Name, &c.Slug, &description, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Category{}, ErrNotFound
	}
	if err != nil {
		return Category{}, fmt.Errorf("scan category: %w", err)
	}

	c.Description = stringOrEmpty(description)
	if c.CreatedAt, err = parseTime(createdAt); err != nil {
		return Category{}, err
	}
	if c.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Category{}, err
	}
	return c, nil
}
