package app

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

// TestResolveEncryptionKeyConfigured confirms a configured PP_ENCRYPTION_KEY is
// hashed to the key and never flagged ephemeral, regardless of the app secret.
func TestResolveEncryptionKeyConfigured(t *testing.T) {
	key, ephemeral := ResolveEncryptionKey("my-encryption-key", []byte("ignored"), true)
	if ephemeral {
		t.Fatal("ResolveEncryptionKey(configured) ephemeral = true, want false")
	}
	if want := sha256.Sum256([]byte("my-encryption-key")); key != want {
		t.Fatal("ResolveEncryptionKey(configured) did not hash the configured key")
	}
}

// TestResolveEncryptionKeyFromAppSecret confirms an empty PP_ENCRYPTION_KEY
// derives the key from the app secret, propagating the app secret's ephemeral
// flag so the caller can warn.
func TestResolveEncryptionKeyFromAppSecret(t *testing.T) {
	appSecret := []byte("resolved-app-secret")

	key, ephemeral := ResolveEncryptionKey("", appSecret, false)
	if ephemeral {
		t.Fatal("ResolveEncryptionKey(stable app secret) ephemeral = true, want false")
	}
	if want := sha256.Sum256(appSecret); key != want {
		t.Fatal("ResolveEncryptionKey(fallback) did not derive from the app secret")
	}

	if _, eph := ResolveEncryptionKey("", appSecret, true); !eph {
		t.Fatal("ResolveEncryptionKey(ephemeral app secret) ephemeral = false, want true")
	}
}

// TestResolvePreviewSecretConfigured confirms a configured PP_APP_SECRET is
// used verbatim and not flagged ephemeral.
func TestResolvePreviewSecretConfigured(t *testing.T) {
	secret, ephemeral, err := ResolvePreviewSecret("my-app-secret")
	if err != nil {
		t.Fatalf("ResolvePreviewSecret() error = %v", err)
	}
	if ephemeral {
		t.Fatal("ResolvePreviewSecret(configured) ephemeral = true, want false")
	}
	if !bytes.Equal(secret, []byte("my-app-secret")) {
		t.Fatalf("ResolvePreviewSecret(configured) = %q, want the configured value", secret)
	}
}

// TestResolvePreviewSecretEphemeral confirms an empty PP_APP_SECRET yields a
// non-empty random secret flagged ephemeral, and that two calls differ.
func TestResolvePreviewSecretEphemeral(t *testing.T) {
	secret, ephemeral, err := ResolvePreviewSecret("")
	if err != nil {
		t.Fatalf("ResolvePreviewSecret() error = %v", err)
	}
	if !ephemeral {
		t.Fatal("ResolvePreviewSecret(\"\") ephemeral = false, want true")
	}
	if len(secret) != ephemeralSecretLen {
		t.Fatalf("ephemeral secret length = %d, want %d", len(secret), ephemeralSecretLen)
	}

	other, _, err := ResolvePreviewSecret("")
	if err != nil {
		t.Fatalf("ResolvePreviewSecret() second call error = %v", err)
	}
	if bytes.Equal(secret, other) {
		t.Fatal("two ephemeral secrets are identical, want independently random values")
	}
}
