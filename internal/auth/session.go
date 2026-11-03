package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

// SessionTTL is how long a session (and its cookie) stays valid after it
// is created: 30 days, per the frozen security decision.
const SessionTTL = 30 * 24 * time.Hour

// sessionTokenBytes is the number of crypto/rand bytes behind each session
// token before base64url encoding — 256 bits of entropy.
const sessionTokenBytes = 32

// newSessionToken returns a fresh opaque session token: 32 crypto/rand
// bytes, base64url-encoded (unpadded) so it is safe to place verbatim in a
// cookie value. The raw token is what the client holds; only its hash (see
// hashToken) is ever stored.
func newSessionToken() (string, error) {
	b := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken returns the SHA-256 hex digest of a session token. The
// sessions table stores this digest, never the token itself, so a database
// disclosure does not hand out usable session cookies. SHA-256 (not a slow
// password hash) is appropriate here because the token already has full
// cryptographic entropy — there is nothing to brute-force.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
