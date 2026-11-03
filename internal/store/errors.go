package store

import "errors"

// ErrNotFound is returned by repository lookups (GetByID, GetByEmail, ...)
// when no matching row exists. Callers compare against it with
// errors.Is; it deliberately carries no row-specific detail so that HTTP
// handlers built on top of this package can map it to a generic 404
// without leaking which table or query produced it.
var ErrNotFound = errors.New("store: not found")
