package content

import (
	"context"
	"log/slog"
	"time"
)

// Scheduler is the background worker that turns scheduled posts public at
// their intended time. It calls Service.PromoteScheduledPosts once
// immediately at startup — which is what makes scheduled publishing survive
// process restarts, since any post that came due while the process was down
// is promoted on the next boot — and then on a fixed interval.
//
// It relies on nothing more than the content service and a ticker; with the
// store's single-writer connection pool (SetMaxOpenConns(1)) a plain
// sequential promotion pass is safe against concurrent HTTP writes.
type Scheduler struct {
	svc      *Service
	interval time.Duration
	logger   *slog.Logger
}

// NewScheduler builds a Scheduler that promotes due posts every interval.
// logger may be nil.
func NewScheduler(svc *Service, interval time.Duration, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Scheduler{svc: svc, interval: interval, logger: logger}
}

// Run promotes overdue posts immediately, then every interval, until ctx is
// cancelled. It is meant to run in its own goroutine for the lifetime of the
// server; cancelling ctx (graceful shutdown) makes it return.
func (s *Scheduler) Run(ctx context.Context) {
	s.promote(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.promote(ctx)
		}
	}
}

// promote runs one promotion pass, logging (but not propagating) a failure —
// a transient store error must not kill the worker; the next tick retries.
func (s *Scheduler) promote(ctx context.Context) {
	promoted, err := s.svc.PromoteScheduledPosts(ctx, time.Now().UTC())
	if err != nil {
		s.logger.Error("scheduled publish pass failed", slog.String("error", err.Error()))
		return
	}
	if len(promoted) > 0 {
		s.logger.Info("scheduled publish pass promoted posts", slog.Int("count", len(promoted)))
	}
}
