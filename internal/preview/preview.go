// Package preview mints and verifies the short-lived, HMAC-signed tokens
// that authorize the public preview endpoint (development plan section 7). A
// token is bound to a single post id and expires 15 minutes after it is
// minted; it carries no server-side state, so verification is a pure
// signature-and-expiry check.
//
// Token layout:
//
//	payload = postID + "." + expiryUnix
//	token   = base64url(payload) + "." + base64url(HMAC-SHA256(secret, payload))
//
// base64url is unpadded (RFC 4648 §5), so the token is URL-safe and the two
// segments are always separated by the single "." between them (the "." inside
// the payload rides along encoded).
package preview

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

// TTL is how long a minted token stays valid.
const TTL = 15 * time.Minute

// ErrInvalidToken is returned by Verify for every rejection — malformed,
// wrong signature, or expired — so a caller cannot distinguish the reasons
// (and neither can an attacker probing the endpoint).
var ErrInvalidToken = errors.New("invalid preview token")

// Tokenizer mints and verifies preview tokens with a fixed HMAC secret.
type Tokenizer struct {
	secret []byte
}

// New builds a Tokenizer over secret (the resolved PP_APP_SECRET).
func New(secret []byte) *Tokenizer {
	return &Tokenizer{secret: secret}
}

// Mint returns a token bound to postID, valid until TTL after now, along with
// that expiry time (truncated to whole seconds, matching the token's
// second-granularity expiry field).
func (t *Tokenizer) Mint(postID string, now time.Time) (token string, expiresAt time.Time) {
	expiresAt = now.Add(TTL).UTC().Truncate(time.Second)
	payload := postID + "." + strconv.FormatInt(expiresAt.Unix(), 10)
	sig := t.sign(payload)
	token = base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		base64.RawURLEncoding.EncodeToString(sig)
	return token, expiresAt
}

// Verify checks token's signature and expiry against now and returns the post
// id it is bound to. Every failure — bad shape, undecodable segment, wrong
// signature, or expiry at/after now — returns ErrInvalidToken. The caller must
// still confirm the returned id matches the post it is serving, so a token for
// one post can never retrieve another.
func (t *Tokenizer) Verify(token string, now time.Time) (postID string, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", ErrInvalidToken
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", ErrInvalidToken
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ErrInvalidToken
	}
	if !hmac.Equal(sig, t.sign(string(payloadBytes))) {
		return "", ErrInvalidToken
	}

	payload := string(payloadBytes)
	dot := strings.LastIndex(payload, ".")
	if dot <= 0 {
		return "", ErrInvalidToken
	}
	postID = payload[:dot]
	expiryUnix, err := strconv.ParseInt(payload[dot+1:], 10, 64)
	if err != nil {
		return "", ErrInvalidToken
	}
	if now.UTC().Unix() >= expiryUnix {
		return "", ErrInvalidToken
	}
	return postID, nil
}

func (t *Tokenizer) sign(payload string) []byte {
	mac := hmac.New(sha256.New, t.secret)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}
