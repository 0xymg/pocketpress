// Package media is PocketPress's media storage domain layer (Phase 4):
// the Driver abstraction over where uploaded files physically live (local
// disk or an S3-compatible bucket), the two Driver implementations, and the
// Service that validates uploads and owns the media table.
//
// Handlers under internal/http/adminapi call Service; no upload validation
// or storage decision is made in the HTTP layer.
package media

import (
	"context"
	"errors"
	"io"
)

// ErrNotFound is returned by Driver.Open when key does not refer to a
// stored object. Driver.Delete treats a missing object as success (delete
// is idempotent), so it never returns ErrNotFound itself.
var ErrNotFound = errors.New("media: object not found")

// Driver is the storage abstraction PocketPress uses to persist uploaded
// media bytes, independent of where they physically live. key is the
// storage-relative path recorded as media.file_key (e.g.
// "2026/08/<id>.png" for the local driver; an S3 object key for the s3
// driver) — never a filesystem-absolute path or a public URL.
type Driver interface {
	// Save writes size bytes read from r under key, creating any
	// intermediate storage location (directory, bucket prefix, ...) as
	// needed. contentType is the sniffed MIME type, passed through so an S3
	// driver can set it on the stored object.
	Save(ctx context.Context, key string, r io.Reader, size int64, contentType string) error

	// Open returns a reader for the object stored under key. The caller
	// must Close it. Returns ErrNotFound if key does not exist.
	Open(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes the object stored under key. It is idempotent: deleting
	// an already-missing key is not an error.
	Delete(ctx context.Context, key string) error
}
