package content

import (
	"context"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
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

// TestSchedulerPromotesAtStartupAndOnTick confirms the worker promotes an
// already-overdue post on its immediate startup pass (restart survival), then
// promotes a post that comes due while it runs on a later tick, and that
// cancelling the context stops it.
func TestSchedulerPromotesAtStartupAndOnTick(t *testing.T) {
	env := newServiceEnv(t)
	now := time.Now().UTC()

	// Overdue before the scheduler starts: must be promoted by the immediate
	// startup pass.
	startupPost := env.newPost(t, "Startup Due")
	past := now.Add(-time.Minute)
	env.forceState(t, startupPost, store.PostStatusScheduled, nil, &past)

	sched := NewScheduler(env.svc, 20*time.Millisecond, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sched.Run(ctx)
		close(done)
	}()

	waitFor(t, "startup post promoted", func() bool {
		return env.row(t, startupPost).Status == store.PostStatusPublished
	})

	// Now seed a second overdue post while the worker is running: a later tick
	// must pick it up.
	tickPost := env.newPost(t, "Tick Due")
	past2 := time.Now().UTC().Add(-time.Second)
	env.forceState(t, tickPost, store.PostStatusScheduled, nil, &past2)

	waitFor(t, "tick post promoted", func() bool {
		return env.row(t, tickPost).Status == store.PostStatusPublished
	})

	// Cancelling the context stops the worker.
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop within 2s of context cancellation")
	}
}
