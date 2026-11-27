package preview

import (
	"strings"
	"testing"
	"time"
)

var testSecret = []byte("preview-unit-test-secret")

// TestMintVerifyRoundTrip confirms a freshly minted token verifies back to
// the post id it was bound to, with the expected 15-minute expiry.
func TestMintVerifyRoundTrip(t *testing.T) {
	tk := New(testSecret)
	now := time.Now().UTC()

	token, expiresAt := tk.Mint("post-123", now)
	if token == "" {
		t.Fatal("Mint returned an empty token")
	}
	if want := now.Add(TTL).Truncate(time.Second); !expiresAt.Equal(want) {
		t.Fatalf("expiresAt = %v, want %v", expiresAt, want)
	}
	// The token has exactly two "."-separated segments.
	if got := strings.Count(token, "."); got != 1 {
		t.Fatalf("token has %d dots, want exactly 1 separator: %q", got, token)
	}

	id, err := tk.Verify(token, now)
	if err != nil {
		t.Fatalf("Verify(fresh) error = %v, want nil", err)
	}
	if id != "post-123" {
		t.Fatalf("Verify id = %q, want post-123", id)
	}
}

// TestVerifyExpired confirms a token is rejected at and after its expiry.
func TestVerifyExpired(t *testing.T) {
	tk := New(testSecret)
	minted := time.Now().UTC()
	token, expiresAt := tk.Mint("post-abc", minted)

	// Just before expiry: still valid.
	if _, err := tk.Verify(token, expiresAt.Add(-time.Second)); err != nil {
		t.Fatalf("Verify just before expiry = %v, want nil", err)
	}
	// Exactly at expiry: rejected (>= comparison).
	if _, err := tk.Verify(token, expiresAt); err != ErrInvalidToken {
		t.Fatalf("Verify at expiry = %v, want ErrInvalidToken", err)
	}
	// Well after expiry: rejected.
	if _, err := tk.Verify(token, expiresAt.Add(time.Hour)); err != ErrInvalidToken {
		t.Fatalf("Verify after expiry = %v, want ErrInvalidToken", err)
	}
}

// TestVerifyTamperedSignature confirms flipping a byte in the signature
// segment fails verification.
func TestVerifyTamperedSignature(t *testing.T) {
	tk := New(testSecret)
	now := time.Now().UTC()
	token, _ := tk.Mint("post-xyz", now)

	i := strings.IndexByte(token, '.')
	sig := []byte(token[i+1:])
	// Flip the first signature character. The last base64url character of a
	// 32-byte HMAC only carries 4 data bits; A↔B can leave the decoded MAC
	// unchanged and make this test flake. The first character always maps
	// to a full 6 bits of the MAC.
	if sig[0] == 'A' {
		sig[0] = 'B'
	} else {
		sig[0] = 'A'
	}
	tampered := token[:i+1] + string(sig)

	if _, err := tk.Verify(tampered, now); err != ErrInvalidToken {
		t.Fatalf("Verify(tampered sig) = %v, want ErrInvalidToken", err)
	}
}

// TestVerifyTamperedPayload confirms changing the encoded post id invalidates
// the token (the signature no longer matches).
func TestVerifyTamperedPayload(t *testing.T) {
	tk := New(testSecret)
	now := time.Now().UTC()
	token, _ := tk.Mint("post-1", now)
	other, _ := tk.Mint("post-2", now)

	// Splice post-2's payload onto post-1's signature.
	spliced := payloadOf(other) + "." + sigOf(token)
	if _, err := tk.Verify(spliced, now); err != ErrInvalidToken {
		t.Fatalf("Verify(spliced payload) = %v, want ErrInvalidToken", err)
	}
}

// TestVerifyWrongSecret confirms a token minted with one secret does not
// verify under another (e.g. after an ephemeral-secret restart).
func TestVerifyWrongSecret(t *testing.T) {
	now := time.Now().UTC()
	token, _ := New(testSecret).Mint("post-1", now)
	if _, err := New([]byte("a-different-secret")).Verify(token, now); err != ErrInvalidToken {
		t.Fatalf("Verify(wrong secret) = %v, want ErrInvalidToken", err)
	}
}

// TestVerifyMalformed confirms structurally broken tokens are rejected.
func TestVerifyMalformed(t *testing.T) {
	tk := New(testSecret)
	now := time.Now().UTC()
	for _, tok := range []string{
		"",
		"no-dot",
		"too.many.dots",
		"!!!.###",             // undecodable base64
		"cG9zdA.notbase64!!!", // bad signature segment
	} {
		if _, err := tk.Verify(tok, now); err != ErrInvalidToken {
			t.Fatalf("Verify(%q) = %v, want ErrInvalidToken", tok, err)
		}
	}
}

func payloadOf(token string) string { return token[:strings.IndexByte(token, '.')] }
func sigOf(token string) string     { return token[strings.IndexByte(token, '.')+1:] }
