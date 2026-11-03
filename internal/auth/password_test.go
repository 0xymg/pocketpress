package auth

import (
	"strings"
	"testing"
)

// TestHashPasswordFormat confirms HashPassword produces a self-contained
// argon2id PHC string with the configured parameters.
func TestHashPasswordFormat(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("hash = %q, want the argon2id PHC prefix", hash)
	}
	if n := strings.Count(hash, "$"); n != 5 {
		t.Fatalf("hash has %d '$' separators, want 5: %q", n, hash)
	}
}

// TestHashPasswordUniqueSalt confirms two hashes of the same password
// differ (fresh per-call salt).
func TestHashPasswordUniqueSalt(t *testing.T) {
	a, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	b, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if a == b {
		t.Fatal("HashPassword() produced identical hashes for the same password; salt is not random")
	}
}

// TestVerifyPassword covers the correct and incorrect password paths.
func TestVerifyPassword(t *testing.T) {
	const password = "a-strong-password"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	ok, err := VerifyPassword(password, hash)
	if err != nil {
		t.Fatalf("VerifyPassword() (correct) error = %v", err)
	}
	if !ok {
		t.Fatal("VerifyPassword() = false for the correct password, want true")
	}

	ok, err = VerifyPassword("wrong-password", hash)
	if err != nil {
		t.Fatalf("VerifyPassword() (wrong) error = %v", err)
	}
	if ok {
		t.Fatal("VerifyPassword() = true for a wrong password, want false")
	}
}

// TestVerifyPasswordMalformed confirms malformed encodings return
// ErrInvalidHash rather than a false positive.
func TestVerifyPasswordMalformed(t *testing.T) {
	for _, enc := range []string{
		"",
		"not-a-hash",
		"$argon2id$v=19$m=19456,t=2,p=1$onlysalt",
		"$bcrypt$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
	} {
		if _, err := VerifyPassword("x", enc); err == nil {
			t.Fatalf("VerifyPassword(%q) error = nil, want a parse error", enc)
		}
	}
}
