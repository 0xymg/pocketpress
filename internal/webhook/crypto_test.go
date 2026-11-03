package webhook

import (
	"context"
	"strings"
	"testing"

	"github.com/pocketpress/pocketpress/internal/store"
)

// TestEncryptDecryptRoundtrip confirms a secret survives an encrypt→decrypt
// roundtrip, that the stored ciphertext never equals the plaintext, and that
// encrypting the same value twice yields different ciphertexts (random nonce).
func TestEncryptDecryptRoundtrip(t *testing.T) {
	c := testCipher()
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret() error = %v", err)
	}

	enc, err := c.Encrypt(secret)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if enc == secret {
		t.Fatal("ciphertext equals plaintext, want them to differ")
	}
	if strings.Contains(enc, secret) {
		t.Fatal("ciphertext contains the plaintext secret")
	}

	got, err := c.Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if got != secret {
		t.Fatalf("Decrypt() = %q, want %q", got, secret)
	}

	enc2, err := c.Encrypt(secret)
	if err != nil {
		t.Fatalf("second Encrypt() error = %v", err)
	}
	if enc == enc2 {
		t.Fatal("two encryptions of the same secret are identical, want distinct nonces")
	}
}

// TestDecryptWrongKeyFails confirms a ciphertext cannot be opened with a
// different key (GCM authentication).
func TestDecryptWrongKeyFails(t *testing.T) {
	enc, err := testCipher().Encrypt("whsec_secret-value")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	other := NewCipher(DeriveKey([]byte("a-completely-different-key")))
	if _, err := other.Decrypt(enc); err == nil {
		t.Fatal("Decrypt() with wrong key succeeded, want an error")
	}
}

// TestStoredSecretIsCiphertext confirms the value persisted in
// webhooks.secret_encrypted is the ciphertext, never the raw secret.
func TestStoredSecretIsCiphertext(t *testing.T) {
	db := newTestDB(t)
	c := testCipher()
	svc := NewService(db, c, NewDispatcher(db, nil), false, nil)

	wh, secret, err := svc.Create(context.Background(), CreateInput{
		Name:   "Rebuild",
		URL:    "https://example.com/hook",
		Events: []string{EventPostPublished},
		Active: true,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !strings.HasPrefix(secret, SecretPrefix) {
		t.Fatalf("secret = %q, want %s… prefix", secret, SecretPrefix)
	}

	row, err := store.GetWebhookByID(context.Background(), db, wh.ID)
	if err != nil {
		t.Fatalf("GetWebhookByID() error = %v", err)
	}
	if row.SecretEncrypted == secret || strings.Contains(row.SecretEncrypted, secret) {
		t.Fatalf("stored secret_encrypted leaks the plaintext: %q", row.SecretEncrypted)
	}
	// And it must decrypt back to the issued secret.
	got, err := c.Decrypt(row.SecretEncrypted)
	if err != nil {
		t.Fatalf("Decrypt(stored) error = %v", err)
	}
	if got != secret {
		t.Fatalf("Decrypt(stored) = %q, want issued secret %q", got, secret)
	}
}
