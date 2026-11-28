package backup

import (
	"context"
	"log/slog"
	"time"
)

// Scheduler is the background worker that creates backup archives on a
// fixed interval while `serve` is running, then prunes older archives down
// to a retention count. It mirrors the content scheduled-publish worker:
// one immediate pass at startup (so a gap while the process was down is
// closed on the next boot), then ticks until ctx is cancelled.
//
// A pass only creates a new archive when the newest existing backup is
// older than Interval (or none exist), so frequent restarts within the
// interval window do not flood <PP_DATA_DIR>/backups.
type Scheduler struct {
	opts     SchedulerOptions
	interval time.Duration
	keep     int
	logger   *slog.Logger
	now      func() time.Time
	create   func(context.Context, CreateOptions) (Result, error)
	list     func(string) ([]Entry, error)
	prune    func(string, int) (PruneResult, error)
}

// SchedulerOptions configures the automatic backup scheduler.
type SchedulerOptions struct {
	// DataDir is PP_DATA_DIR (database + uploads + backups/).
	DataDir string
	// Version is recorded in each archive's manifest.
	Version string
	// StorageDriver is recorded in each archive's manifest ("local" or "s3").
	StorageDriver string
	// Interval is how old the newest backup must be before a new one is created.
	Interval time.Duration
	// Keep is how many newest archives to retain after each successful create.
	Keep int
}

// NewScheduler builds a Scheduler. logger may be nil.
func NewScheduler(opts SchedulerOptions, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Scheduler{
		opts:     opts,
		interval: opts.Interval,
		keep:     opts.Keep,
		logger:   logger,
		now:      func() time.Time { return time.Now().UTC() },
		create:   Create,
		list:     List,
		prune:    Prune,
	}
}

// Run runs one backup pass immediately, then every interval, until ctx is
// cancelled. It is meant to run in its own goroutine for the lifetime of
// the server; cancelling ctx (graceful shutdown) makes it return.
func (s *Scheduler) Run(ctx context.Context) {
	s.pass(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pass(ctx)
		}
	}
}

// pass creates a backup when due, then prunes. Failures are logged and do
// not kill the worker — the next tick retries.
func (s *Scheduler) pass(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	due, err := s.backupDue()
	if err != nil {
		s.logger.Error("automatic backup: list failed", slog.String("error", err.Error()))
		return
	}
	if !due {
		return
	}

	result, err := s.create(ctx, CreateOptions{
		DataDir:       s.opts.DataDir,
		Version:       s.opts.Version,
		StorageDriver: s.opts.StorageDriver,
	})
	if err != nil {
		s.logger.Error("automatic backup: create failed", slog.String("error", err.Error()))
		return
	}
	s.logger.Info("automatic backup created",
		slog.String("path", result.Path),
		slog.Int64("size", result.Size),
	)

	pruned, err := s.prune(s.opts.DataDir, s.keep)
	if err != nil {
		s.logger.Error("automatic backup: prune failed", slog.String("error", err.Error()))
		return
	}
	if pruned.Removed > 0 {
		s.logger.Info("automatic backup pruned",
			slog.Int("removed", pruned.Removed),
			slog.Int("kept", pruned.Kept),
		)
	}
}

// backupDue reports whether a new archive should be created: true when the
// backups directory is empty or the newest archive's CreatedAt is at least
// Interval old.
func (s *Scheduler) backupDue() (bool, error) {
	entries, err := s.list(s.opts.DataDir)
	if err != nil {
		return false, err
	}
	if len(entries) == 0 {
		return true, nil
	}
	age := s.now().Sub(entries[0].CreatedAt)
	return age >= s.interval, nil
}
