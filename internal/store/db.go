package store

import (
	"context"
	"database/sql"
)

// DBTX is the subset of *sql.DB the repositories need. Both *sql.DB and
// *sql.Tx satisfy it, so the same repository function can run either
// directly against the pool or inside a transaction — the content service
// uses this to make a multi-table post write (post row + category/tag joins
// + revision) atomic without the store package having to know about
// transactions itself.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
