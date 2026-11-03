// Package webhook implements PocketPress's outbound webhook subsystem
// (development plan sections 8 and 10): it encrypts signing secrets at rest,
// fans events out into a durable, DB-backed delivery queue, and delivers them
// with HMAC signatures, a fixed retry schedule and SSRF protections.
package webhook

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// secretBytes is the number of random bytes behind a webhook signing secret.
const secretBytes = 32

// SecretPrefix marks a PocketPress webhook signing secret, mirroring the
// widely-recognized "whsec_" convention so receivers can spot it.
const SecretPrefix = "whsec_"

// Cipher encrypts and decrypts webhook signing secrets at rest with
// AES-256-GCM. The 32-byte key is derived from the configured encryption key
// material via SHA-256 (see DeriveKey).
type Cipher struct {
	key [32]byte
}

// NewCipher builds a Cipher over a 32-byte AES-256 key.
func NewCipher(key [32]byte) *Cipher {
	return &Cipher{key: key}
}

// DeriveKey turns arbitrary key material (PP_ENCRYPTION_KEY, or the app secret
// fallback) into a fixed 32-byte AES-256 key via SHA-256, so any non-empty
// string is accepted as a key.
func DeriveKey(material []byte) [32]byte {
	return sha256.Sum256(material)
}

// Encrypt seals plaintext with AES-256-GCM under a fresh random nonce and
// returns base64(nonce || ciphertext||tag). The random nonce is prepended so
// Decrypt can recover it; encrypting the same secret twice yields different
// stored values, and the stored value never equals the plaintext.
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	gcm, err := c.gcm()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt: it base64-decodes stored, splits off the prepended
// nonce, and opens the AES-256-GCM ciphertext. A wrong key or tampered value
// fails the GCM authentication check and returns an error.
func (c *Cipher) Decrypt(stored string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return "", fmt.Errorf("decode webhook secret: %w", err)
	}
	gcm, err := c.gcm()
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("decrypt webhook secret: ciphertext too short")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt webhook secret: %w", err)
	}
	return string(plaintext), nil
}

// gcm builds the AES-256-GCM AEAD for this cipher's key. A 32-byte key is
// always valid for AES-256, so this only errors on an impossible internal
// failure.
func (c *Cipher) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(c.key[:])
	if err != nil {
		return nil, fmt.Errorf("build aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build gcm: %w", err)
	}
	return gcm, nil
}

// GenerateSecret mints a new raw webhook signing secret: "whsec_" followed by
// base64url(32 random bytes). It is returned to the admin exactly once (at
// create or rotate) and never stored in the clear.
func GenerateSecret() (string, error) {
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate webhook secret: %w", err)
	}
	return SecretPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}
