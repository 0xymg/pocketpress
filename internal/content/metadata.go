package content

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Metadata size / shape limits for the per-post custom metadata object.
const (
	emptyMetadata    = "{}"
	metadataMaxBytes = 16 << 10 // 16 KiB serialized
	metadataMaxDepth = 8
	metadataMaxKeys  = 100 // top-level keys only
)

// NormalizeMetadata validates and canonicalizes a post's custom metadata
// object. Blank / nil input becomes "{}". The value must be a JSON object
// (not an array, scalar, or null), at most metadataMaxBytes, with at most
// metadataMaxKeys top-level keys and nesting depth metadataMaxDepth.
//
// On success it returns compact canonical JSON. On failure it returns a
// user-facing error message (empty string means OK) suitable for a
// validation field error under "metadata".
func NormalizeMetadata(raw []byte) (normalized string, errMsg string) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return emptyMetadata, ""
	}
	if len(trimmed) > metadataMaxBytes {
		return "", fmt.Sprintf("Metadata must be at most %d bytes.", metadataMaxBytes)
	}
	if !json.Valid(trimmed) {
		return "", "Metadata must be valid JSON."
	}

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", "Metadata must be valid JSON."
	}
	if dec.More() {
		return "", "Metadata must be a single JSON object."
	}

	obj, ok := v.(map[string]any)
	if !ok {
		return "", "Metadata must be a JSON object."
	}
	if len(obj) > metadataMaxKeys {
		return "", fmt.Sprintf("Metadata may have at most %d top-level keys.", metadataMaxKeys)
	}
	if depth := jsonDepth(v); depth > metadataMaxDepth {
		return "", fmt.Sprintf("Metadata nesting depth must be at most %d.", metadataMaxDepth)
	}

	out, err := json.Marshal(obj)
	if err != nil {
		return "", "Metadata must be valid JSON."
	}
	if len(out) > metadataMaxBytes {
		return "", fmt.Sprintf("Metadata must be at most %d bytes.", metadataMaxBytes)
	}
	return string(out), ""
}

// jsonDepth returns the maximum nesting depth of a decoded JSON value.
// Scalars and empty containers have depth 1; each nested container adds 1.
func jsonDepth(v any) int {
	switch t := v.(type) {
	case map[string]any:
		maxChild := 0
		for _, child := range t {
			if d := jsonDepth(child); d > maxChild {
				maxChild = d
			}
		}
		return 1 + maxChild
	case []any:
		maxChild := 0
		for _, child := range t {
			if d := jsonDepth(child); d > maxChild {
				maxChild = d
			}
		}
		return 1 + maxChild
	default:
		return 1
	}
}

// MetadataOrEmpty returns m when non-empty, otherwise "{}".
func MetadataOrEmpty(m string) string {
	if m == "" {
		return emptyMetadata
	}
	return m
}
