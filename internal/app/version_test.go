package app

import "testing"

func TestVersionIsSet(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}

	if Version != "0.1.0" {
		t.Fatalf("Version = %q, want %q (override via -ldflags if this is intentional)", Version, "0.1.0")
	}
}
