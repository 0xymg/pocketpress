package backup

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// waitFor polls cond up to a deadline, failing the test if it never holds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition never held: %s", what)
}

// TestSchedulerCreatesWhenDueAndPrunes is the happy path: an empty backups
// directory is due immediately, Create runs, then Prune runs with the
// configured keep count. A later tick with a fresh archive is a no-op.
func TestSchedulerCreatesWhenDueAndPrunes(t *testing.T) {
	var (
		mu       sync.Mutex
		creates  int
		prunes   int
		lastKeep int
		entries  []Entry
	)

	sched := NewScheduler(SchedulerOptions{
		DataDir:       t.TempDir(),
		Version:       "test",
		StorageDriver: "local",
		Interval:      50 * time.Millisecond,
		Keep:          3,
	}, slog.New(slog.DiscardHandler))

	sched.now = func() time.Time { return time.Now().UTC() }
	sched.list = func(string) ([]Entry, error) {
		mu.Lock()
		defer mu.Unlock()
		out := make([]Entry, len(entries))
		copy(out, entries)
		return out, nil
	}
	sched.create = func(context.Context, CreateOptions) (Result, error) {
		mu.Lock()
		defer mu.Unlock()
		creates++
		entries = append([]Entry{{
			Name:      "new.zip",
			Path:      "/tmp/new.zip",
			CreatedAt: time.Now().UTC(),
		}}, entries...)
		return Result{Path: "/tmp/new.zip", Size: 42}, nil
	}
	sched.prune = func(_ string, keep int) (PruneResult, error) {
		mu.Lock()
		defer mu.Unlock()
		prunes++
		lastKeep = keep
		return PruneResult{Kept: keep}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sched.Run(ctx)
		close(done)
	}()

	waitFor(t, "create+prune on startup", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return creates >= 1 && prunes >= 1
	})

	mu.Lock()
	if lastKeep != 3 {
		t.Fatalf("prune keep = %d, want 3", lastKeep)
	}
	startupCreates := creates
	mu.Unlock()

	// Within the interval window the next tick must not create again.
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	if creates != startupCreates {
		t.Fatalf("creates = %d after short wait, want still %d (not due yet)", creates, startupCreates)
	}
	mu.Unlock()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop within 2s of context cancellation")
	}
}

// TestSchedulerSurvivesCreateFailure confirms a failed Create is logged and
// retried on a later tick rather than killing the worker.
func TestSchedulerSurvivesCreateFailure(t *testing.T) {
	var (
		attempts atomic.Int32
		success  atomic.Bool
	)

	sched := NewScheduler(SchedulerOptions{
		DataDir:  t.TempDir(),
		Interval: 20 * time.Millisecond,
		Keep:     1,
	}, nil)

	sched.list = func(string) ([]Entry, error) { return nil, nil } // always due
	sched.create = func(context.Context, CreateOptions) (Result, error) {
		n := attempts.Add(1)
		if n == 1 {
			return Result{}, errors.New("disk full")
		}
		success.Store(true)
		return Result{Path: "/tmp/ok.zip", Size: 1}, nil
	}
	sched.prune = func(string, int) (PruneResult, error) {
		return PruneResult{Kept: 1}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sched.Run(ctx)
		close(done)
	}()

	waitFor(t, "retry after failure", func() bool {
		return success.Load() && attempts.Load() >= 2
	})

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop within 2s of context cancellation")
	}
}

// TestSchedulerSkipsWhenNewestIsFresh confirms the due-check permission to
// create: a recent archive within the interval means Create is not called.
func TestSchedulerSkipsWhenNewestIsFresh(t *testing.T) {
	var creates atomic.Int32

	sched := NewScheduler(SchedulerOptions{
		DataDir:  t.TempDir(),
		Interval: time.Hour,
		Keep:     7,
	}, nil)

	sched.now = func() time.Time { return time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC) }
	sched.list = func(string) ([]Entry, error) {
		return []Entry{{
			Name:      "fresh.zip",
			CreatedAt: time.Date(2026, 11, 28, 11, 30, 0, 0, time.UTC), // 30m old
		}}, nil
	}
	sched.create = func(context.Context, CreateOptions) (Result, error) {
		creates.Add(1)
		return Result{}, nil
	}
	sched.prune = func(string, int) (PruneResult, error) {
		t.Fatal("prune should not run when create is skipped")
		return PruneResult{}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sched.Run(ctx)
		close(done)
	}()

	time.Sleep(40 * time.Millisecond)
	if creates.Load() != 0 {
		t.Fatalf("creates = %d, want 0 when newest backup is still fresh", creates.Load())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop within 2s of context cancellation")
	}
}
