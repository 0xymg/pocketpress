package apikey

import (
	"strings"
	"testing"
)

func TestGenerateKeyFormat(t *testing.T) {
	raw, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	if !strings.HasPrefix(raw, KeyPrefix) {
		t.Fatalf("key = %q, want %s… prefix", raw, KeyPrefix)
	}
	if len(raw) <= displayPrefixLen {
		t.Fatalf("key too short: %q", raw)
	}
	prefix := DisplayPrefix(raw)
	if prefix != raw[:displayPrefixLen] {
		t.Fatalf("DisplayPrefix = %q, want %q", prefix, raw[:displayPrefixLen])
	}
	if !LooksLikeKey(raw) {
		t.Fatalf("LooksLikeKey(%q) = false", raw)
	}
	if LooksLikeKey("whsec_abc") {
		t.Fatal("LooksLikeKey(whsec_…) = true, want false")
	}
}

func TestHashStableAndDistinct(t *testing.T) {
	a, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	ha1 := Hash(a)
	ha2 := Hash(a)
	hb := Hash(b)
	if ha1 != ha2 {
		t.Fatalf("Hash not stable: %q vs %q", ha1, ha2)
	}
	if ha1 == hb {
		t.Fatal("distinct keys hashed to the same digest")
	}
	if len(ha1) != 64 {
		t.Fatalf("hash len = %d, want 64 hex chars", len(ha1))
	}
	if EqualHash(ha1, hb) {
		t.Fatal("EqualHash reported equal for distinct hashes")
	}
	if !EqualHash(ha1, ha2) {
		t.Fatal("EqualHash reported unequal for identical hashes")
	}
}
