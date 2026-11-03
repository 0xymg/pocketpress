package media

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ValidationError carries field-level validation failures from the media
// service (an unacceptable upload, ...), mirroring content.ValidationError
// so the HTTP layer maps both the same way.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %s", k, e.Fields[k]))
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// fieldError builds a *ValidationError carrying a single field message, the
// shape every upload validation failure takes (the frozen contract only
// ever reports upload problems under the "file" field).
func fieldError(field, message string) error {
	return &ValidationError{Fields: map[string]string{field: message}}
}

// ErrMediaInUse is returned by Service.Delete when the media is still
// referenced by a post's cover_image or og_image.
var ErrMediaInUse = errors.New("media: still referenced by a post")
