package content

import (
	"fmt"
	"sort"
	"strings"
)

// ValidationError carries one or more field-level validation failures raised
// by the content service (required/length checks, slug rules, unknown
// references, the Phase 3 status guard, …). HTTP handlers map it to the
// frozen validation_failed envelope, using Fields directly as the details
// map (field name → human message).
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

// fieldErrors accumulates field validation messages while a create/update is
// checked, then materializes into a *ValidationError (or nil when clean).
type fieldErrors map[string]string

func (f fieldErrors) set(field, message string) {
	// Keep the first message recorded for a field; earlier checks are the
	// more fundamental ones (e.g. "required" before "already in use").
	if _, ok := f[field]; !ok {
		f[field] = message
	}
}

func (f fieldErrors) has(field string) bool {
	_, ok := f[field]
	return ok
}

func (f fieldErrors) orNil() error {
	if len(f) == 0 {
		return nil
	}
	return &ValidationError{Fields: map[string]string(f)}
}
