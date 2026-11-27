// Package apikey implements PocketPress API key management for private
// content API access (development plan §16 v0.2 and the api_keys schema in
// §5): mint high-entropy keys, store only SHA-256 hashes, and authorize
// Bearer tokens against scopes content:read / preview:read.
package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// KeyPrefix marks a PocketPress content API key so operators and clients can
// spot it (mirroring whsec_ for webhook secrets).
const KeyPrefix = "ppkey_"

// secretBytes is the entropy behind the random portion of an API key.
const secretBytes = 32

// displayPrefixLen is how many characters of the full key (including the
// ppkey_ marker) are stored as the display prefix. Long enough to tell keys
// apart in the admin UI, short enough not to leak the full secret.
const displayPrefixLen = 12

// GenerateKey mints a new raw API key: "ppkey_" followed by base64url(32
// random bytes). The raw value is returned to the admin exactly once at
// creation and never stored in the clear.
func GenerateKey() (raw string, err error) {
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate api key: %w", err)
	}
	return KeyPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// DisplayPrefix returns the short identifier stored alongside the hash so
// operators can recognize a key without seeing the full secret.
func DisplayPrefix(raw string) string {
	if len(raw) <= displayPrefixLen {
		return raw
	}
	return raw[:displayPrefixLen]
}

// Hash returns the SHA-256 hex digest of a raw API key. The api_keys table
// stores this digest, never the key itself. SHA-256 (not a slow password
// hash) is appropriate because the key already has full cryptographic
// entropy — same rationale as session tokens in internal/auth.
func Hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// LooksLikeKey reports whether s has the PocketPress API key prefix, so
// middleware can decide whether a Bearer token should be treated as an API
// key (vs ignored when the public API is open).
func LooksLikeKey(s string) bool {
	return strings.HasPrefix(s, KeyPrefix)
}

// EqualHash reports whether a and b are equal token hashes in constant time.
func EqualHash(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
