package webhook

import "testing"

// TestSignKnownVector locks the signature format to a precomputed HMAC-SHA256
// vector for a fixed secret, timestamp and body, so an accidental change to the
// signed string (order, separator, encoding) is caught.
func TestSignKnownVector(t *testing.T) {
	secret := []byte("whsec_testsecret")
	timestamp := int64(1700000000)
	body := []byte(`{"hello":"world"}`)

	const want = "sha256=40635f979692c953d82b075bbd75d7d14b7710bacc33c3ec5cb62604cc901a3a"
	got := Sign(secret, timestamp, body)
	if got != want {
		t.Fatalf("Sign() = %q, want %q", got, want)
	}
}

// TestSignTimestampBinding confirms the timestamp is part of the signed string,
// so two timestamps over the same body produce different signatures.
func TestSignTimestampBinding(t *testing.T) {
	secret := []byte("whsec_testsecret")
	body := []byte(`{"a":1}`)
	if Sign(secret, 1, body) == Sign(secret, 2, body) {
		t.Fatal("signatures for different timestamps are equal, want them to differ")
	}
}
