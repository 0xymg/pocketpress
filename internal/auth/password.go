// Package auth is PocketPress's authentication and first-run setup domain
// layer: argon2id password hashing (password.go), opaque server-side
// session tokens (session.go), and the Service (service.go) that first-run
// setup, login/logout, and owner user management call. HTTP handlers in
// internal/http/adminapi stay thin by delegating every decision here.
//
// Nothing in this package writes to http.ResponseWriter or reads
// *http.Request: it works in terms of plain inputs and store types so it
// can be unit-tested against a real SQLite store opened on a t.TempDir().
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters. These follow the OWASP Password Storage Cheat
// Sheet's recommended argon2id configuration (m=19456 KiB, t=2, p=1) with
// a 16-byte salt and 32-byte derived key. They are the parameters used
// when hashing new passwords; verification reads the parameters back out
// of the stored PHC string instead of assuming these, so existing hashes
// keep verifying if these constants are later tuned.
const (
	argonMemoryKiB   = 19456
	argonTime        = 2
	argonParallelism = 1
	argonSaltLen     = 16
	argonKeyLen      = 32
)

// ErrInvalidHash is returned by VerifyPassword when the stored value is
// not a well-formed argon2id PHC string.
var ErrInvalidHash = errors.New("auth: invalid password hash format")

// ErrIncompatibleVersion is returned by VerifyPassword when the stored
// hash was produced by a different argon2 algorithm version than this
// build links against.
var ErrIncompatibleVersion = errors.New("auth: incompatible argon2 version")

// HashPassword hashes password with argon2id and returns a self-contained
// PHC-format string:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<base64 salt>$<base64 hash>
//
// The salt is a fresh 16 crypto/rand bytes per call, so hashing the same
// password twice yields different strings. The encoded string carries
// every parameter needed to verify it later.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonParallelism, argonKeyLen)

	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonParallelism,
		b64.EncodeToString(salt), b64.EncodeToString(hash),
	), nil
}

// VerifyPassword reports whether password matches the argon2id PHC string
// encoded. It parses the parameters, salt and key length out of encoded
// (rather than assuming the current constants), recomputes the hash and
// compares it in constant time.
//
// A false result with a nil error means "password does not match." A
// non-nil error means encoded could not be parsed (ErrInvalidHash) or was
// produced by an incompatible argon2 version (ErrIncompatibleVersion); the
// bool is false in those cases too.
func VerifyPassword(password, encoded string) (bool, error) {
	// Format: ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash].
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, ErrInvalidHash
	}
	if version != argon2.Version {
		return false, ErrIncompatibleVersion
	}

	var (
		memory      uint32
		timeCost    uint32
		parallelism uint8
	)
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &parallelism); err != nil {
		return false, ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, ErrInvalidHash
	}

	got := argon2.IDKey([]byte(password), salt, timeCost, memory, parallelism, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) == 1 {
		return true, nil
	}
	return false, nil
}
