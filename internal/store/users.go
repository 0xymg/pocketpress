package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// User roles. Permissions are enforced by the (future) auth/handler layer,
// not by this package; role is stored as plain TEXT, constrained by the
// users.role CHECK in the 0001_init migration.
const (
	RoleOwner  = "owner"
	RoleEditor = "editor"
)

// User is a row of the users table.
type User struct {
	ID           string
	Email        string
	PasswordHash string
	Name         string
	Role         string
	Avatar       string // "" means unset (NULL in storage)
	Active       bool
	LastLoginAt  *time.Time // nil means never logged in (NULL in storage)
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewUser holds the fields a caller supplies to CreateUser. ID,
// CreatedAt, UpdatedAt and Active are set by CreateUser itself: Active is
// always true for a newly created user (deactivate afterwards via
// SetUserActive), matching the users.active DEFAULT in the schema.
type NewUser struct {
	Email        string
	PasswordHash string
	Name         string
	Role         string
	Avatar       string
}

// CreateUser inserts a new user and returns the row as stored, including
// the generated ID and timestamps.
//
// A duplicate email (case-insensitive, per the users.email COLLATE NOCASE
// UNIQUE constraint) returns the underlying SQLite constraint error
// unwrapped; callers that need to distinguish "email already taken" from
// other failures should check for that constraint by message or rely on a
// pre-flight GetUserByEmail — this package does not layer a sentinel error
// on top of the raw SQLite error here, to avoid hiding which constraint
// fired.
func CreateUser(ctx context.Context, db *sql.DB, in NewUser) (User, error) {
	id, err := newID()
	if err != nil {
		return User{}, err
	}

	now := time.Now().UTC()
	u := User{
		ID:           id,
		Email:        in.Email,
		PasswordHash: in.PasswordHash,
		Name:         in.Name,
		Role:         in.Role,
		Avatar:       in.Avatar,
		Active:       true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO users (id, email, password_hash, name, role, avatar, active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Email, u.PasswordHash, u.Name, u.Role, nullString(u.Avatar), boolToInt(u.Active),
		formatTime(u.CreatedAt), formatTime(u.UpdatedAt),
	)
	if err != nil {
		return User{}, fmt.Errorf("insert user: %w", err)
	}

	return u, nil
}

// GetUserByEmail looks up a user by email. The comparison is
// case-insensitive because the users.email column is declared
// COLLATE NOCASE. Returns ErrNotFound if no user has that email.
func GetUserByEmail(ctx context.Context, db *sql.DB, email string) (User, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, email, password_hash, name, role, avatar, active, last_login_at, created_at, updated_at
		FROM users
		WHERE email = ?`,
		email,
	)
	return scanUser(row)
}

// GetUserByID looks up a user by ID. Returns ErrNotFound if no such user
// exists.
func GetUserByID(ctx context.Context, db *sql.DB, id string) (User, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, email, password_hash, name, role, avatar, active, last_login_at, created_at, updated_at
		FROM users
		WHERE id = ?`,
		id,
	)
	return scanUser(row)
}

// UpdateUserLastLogin sets last_login_at for the given user to when.
// Returns ErrNotFound if no such user exists.
func UpdateUserLastLogin(ctx context.Context, db *sql.DB, id string, when time.Time) error {
	res, err := db.ExecContext(ctx,
		`UPDATE users SET last_login_at = ? WHERE id = ?`,
		formatTime(when), id,
	)
	if err != nil {
		return fmt.Errorf("update user last_login_at: %w", err)
	}
	return requireRowsAffected(res)
}

// SetUserActive sets a user's active flag (and bumps updated_at). Returns
// ErrNotFound if no such user exists.
func SetUserActive(ctx context.Context, db *sql.DB, id string, active bool) error {
	res, err := db.ExecContext(ctx,
		`UPDATE users SET active = ?, updated_at = ? WHERE id = ?`,
		boolToInt(active), formatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("update user active: %w", err)
	}
	return requireRowsAffected(res)
}

// SetUserName updates a user's display name (and bumps updated_at).
// Returns ErrNotFound if no such user exists.
func SetUserName(ctx context.Context, db *sql.DB, id, name string) error {
	res, err := db.ExecContext(ctx,
		`UPDATE users SET name = ?, updated_at = ? WHERE id = ?`,
		name, formatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("update user name: %w", err)
	}
	return requireRowsAffected(res)
}

// SetUserRole updates a user's role (and bumps updated_at). The role must
// be one of the values allowed by the users.role CHECK constraint
// (RoleOwner, RoleEditor); an invalid role surfaces as the underlying
// SQLite constraint error. Returns ErrNotFound if no such user exists.
func SetUserRole(ctx context.Context, db *sql.DB, id, role string) error {
	res, err := db.ExecContext(ctx,
		`UPDATE users SET role = ?, updated_at = ? WHERE id = ?`,
		role, formatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("update user role: %w", err)
	}
	return requireRowsAffected(res)
}

// SetUserPasswordHash replaces a user's stored password hash (and bumps
// updated_at). The caller is responsible for hashing; this stores the
// already-encoded value verbatim. Returns ErrNotFound if no such user
// exists.
func SetUserPasswordHash(ctx context.Context, db *sql.DB, id, passwordHash string) error {
	res, err := db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		passwordHash, formatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("update user password hash: %w", err)
	}
	return requireRowsAffected(res)
}

// ListUsers returns every user ordered by creation time (oldest first,
// i.e. the owner created during setup sorts first).
func ListUsers(ctx context.Context, db *sql.DB) ([]User, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, email, password_hash, name, role, avatar, active, last_login_at, created_at, updated_at
		FROM users
		ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}

	return users, nil
}

// CountUsersByRole returns the number of users with the given role,
// regardless of active status.
func CountUsersByRole(ctx context.Context, db *sql.DB, role string) (int, error) {
	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE role = ?`, role,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count users by role: %w", err)
	}
	return count, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows, letting
// scanUser share one implementation between GetUserBy* (single row) and
// ListUsers (multiple rows).
type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (User, error) {
	return scanUserRow(row)
}

func scanUserRow(row rowScanner) (User, error) {
	var (
		u           User
		avatar      sql.NullString
		active      int
		lastLoginAt sql.NullString
		createdAt   string
		updatedAt   string
	)

	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Role, &avatar, &active, &lastLoginAt, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("scan user: %w", err)
	}

	u.Avatar = stringOrEmpty(avatar)
	u.Active = active != 0

	if u.CreatedAt, err = parseTime(createdAt); err != nil {
		return User{}, err
	}
	if u.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return User{}, err
	}
	if lastLoginAt.Valid {
		t, err := parseTime(lastLoginAt.String)
		if err != nil {
			return User{}, err
		}
		u.LastLoginAt = &t
	}

	return u, nil
}

// requireRowsAffected returns ErrNotFound if res reports zero rows
// affected, which for the single-row UPDATE ... WHERE id = ? statements
// in this package means "no row with that id existed."
func requireRowsAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
