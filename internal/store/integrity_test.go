package store

import (
	"context"
	"strings"
	"testing"
)

// TestQuickCheckHealthy confirms a freshly-opened, migrated database passes the
// integrity check.
func TestQuickCheckHealthy(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer st.Close()

	if _, err := Up(context.Background(), st.DB()); err != nil {
		t.Fatalf("Up() error = %v", err)
	}

	if err := QuickCheck(context.Background(), st.DB(), 1); err != nil {
		t.Fatalf("QuickCheck() on a healthy database = %v, want nil", err)
	}
}

// TestInterpretQuickCheck exercises the result-interpretation logic directly.
// The corruption branch is tested here (rather than by fabricating a corrupt
// SQLite file, which the driver does not surface deterministically) by feeding
// interpretQuickCheck the rows quick_check would return: a single "ok" row is
// sound, anything else is corruption.
func TestInterpretQuickCheck(t *testing.T) {
	if err := interpretQuickCheck([]string{"ok"}); err != nil {
		t.Fatalf("interpretQuickCheck([ok]) = %v, want nil", err)
	}

	err := interpretQuickCheck([]string{"*** in database main ***", "Page 4 is never used"})
	if err == nil {
		t.Fatal("interpretQuickCheck(corruption rows) = nil, want an error")
	}
	if !strings.Contains(err.Error(), "Page 4 is never used") {
		t.Fatalf("error = %q, want it to include the reported problem", err.Error())
	}

	if err := interpretQuickCheck(nil); err == nil {
		t.Fatal("interpretQuickCheck(no rows) = nil, want an error")
	}
}
