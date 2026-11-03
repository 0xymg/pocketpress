// Package app wires together the PocketPress executable: configuration,
// structured logging, and the HTTP server lifecycle.
//
// PocketPress is a standalone Go application (no external framework). This
// package owns process-level concerns — building the logger, creating the
// data directory, starting/stopping the HTTP server — while the actual
// HTTP routing lives in internal/http so it can be built and tested
// independently of process lifecycle.
package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/store"
)

// Runner is a background goroutine Serve supervises for the server's lifetime,
// stopping (and waiting on) it during graceful shutdown. The webhook delivery
// worker is one; it is passed as an interface so this package does not import
// internal/webhook.
type Runner interface {
	Run(ctx context.Context)
}

// Components are the process-wide runtime pieces Serve wires up once the store
// is open: the HTTP handler, the shared content service (so the scheduled-
// publish worker promotes posts through the same service the request handlers
// use — and therefore fires the same webhook events), and the webhook delivery
// worker. Content and Worker may be nil (e.g. in tests that only exercise the
// HTTP lifecycle), in which case Serve simply does not start that goroutine.
type Components struct {
	Handler http.Handler
	Content *content.Service
	Worker  Runner
}

const (
	// HTTP server timeouts. ReadHeaderTimeout in particular guards against
	// slow-header (Slowloris-style) connections holding a goroutine open
	// indefinitely.
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second

	// shutdownTimeout bounds how long graceful shutdown waits for
	// in-flight requests to finish before giving up.
	shutdownTimeout = 10 * time.Second

	dataDirPerm = 0o755

	// schedulerInterval is how often the scheduled-publish worker checks for
	// posts that have come due. A due post becomes public within this window
	// of its scheduled_for; the worker also runs once immediately at startup,
	// which is what makes scheduled publishing survive restarts.
	schedulerInterval = 30 * time.Second

	// ephemeralSecretLen is the byte length of the random preview-token secret
	// generated when PP_APP_SECRET is unset.
	ephemeralSecretLen = 32

	// integrityCheckLimit bounds how many problems PRAGMA quick_check reports
	// at startup before giving up — one is enough to fail fast.
	integrityCheckLimit = 1
)

// NewLogger builds a structured slog.Logger for the given configuration.
//
// Production environments get a JSON handler (machine-parseable, suitable
// for log aggregation); every other environment gets a human-readable text
// handler. The minimum level is taken from Config.LogLevel.
func NewLogger(cfg Config) *slog.Logger {
	handlerOpts := &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}

	var handler slog.Handler
	if cfg.IsProduction() {
		handler = slog.NewJSONHandler(os.Stdout, handlerOpts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, handlerOpts)
	}

	return slog.New(handler)
}

// parseLogLevel converts the PP_LOG_LEVEL string into a slog.Level,
// defaulting to Info for unrecognized values.
func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Serve runs the PocketPress HTTP server until ctx is cancelled (e.g. by
// signal.NotifyContext reacting to SIGINT/SIGTERM in the caller) or the
// server fails to start, then performs a bounded graceful shutdown.
//
// It creates cfg.DataDir if it does not already exist yet, opens the
// SQLite store inside it and applies any pending schema migrations,
// logging how many were applied, before ever binding the HTTP listener —
// startup fails fast (returns an error, never starts serving) if opening
// the store or migrating fails. Once the store is open and migrated, Serve
// builds the HTTP handler via newHandler, passing it the open database
// handle so the handler's data-backed routes (the admin API) can reach the
// store. The store is closed on every return path, including a failed
// listen. It also logs a structured startup message with the bound
// address, and logs again once shutdown completes.
func Serve(ctx context.Context, cfg Config, logger *slog.Logger, build func(db *sql.DB) Components) error {
	if err := os.MkdirAll(cfg.DataDir, dataDirPerm); err != nil {
		return fmt.Errorf("create data dir %q: %w", cfg.DataDir, err)
	}

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			logger.Error("failed to close store", slog.String("error", err.Error()))
		}
	}()

	// Fail fast on a structurally corrupt database before touching it: a
	// PRAGMA quick_check that reports anything other than "ok" aborts startup
	// rather than letting migrations or request handlers operate on a damaged
	// file. A healthy (including brand-new, empty) database passes silently.
	if err := store.QuickCheck(ctx, st.DB(), integrityCheckLimit); err != nil {
		return fmt.Errorf("database integrity check: %w", err)
	}
	logger.Info("database integrity check passed")

	applied, err := store.Up(ctx, st.DB())
	if err != nil {
		return fmt.Errorf("apply database migrations: %w", err)
	}
	logger.Info("database migrations applied", slog.Int("count", len(applied)))

	comps := build(st.DB())

	ln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on %q: %w", cfg.HTTPAddr, err)
	}

	// Start the scheduled-publish worker over the SHARED content service so a
	// scheduler promotion fires the same webhook events an HTTP publish does.
	// It promotes any overdue posts once immediately — the mechanism that lets
	// scheduled publishing survive a restart — then on a fixed interval.
	if comps.Content != nil {
		stopScheduler := runBackground(ctx, func(runCtx context.Context) {
			content.NewScheduler(comps.Content, schedulerInterval, logger).Run(runCtx)
		})
		defer stopScheduler()
	}

	// Start the webhook delivery worker, which drains the DB-backed delivery
	// queue until shutdown.
	if comps.Worker != nil {
		stopWorker := runBackground(ctx, comps.Worker.Run)
		defer stopWorker()
	}

	srv := &http.Server{
		Handler:           comps.Handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	logger.Info("starting pocketpress",
		slog.String("version", Version),
		slog.String("env", cfg.Env),
		slog.String("http_addr", ln.Addr().String()),
		slog.String("data_dir", cfg.DataDir),
	)

	serveErrCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrCh <- err
			return
		}
		serveErrCh <- nil
	}()

	select {
	case err := <-serveErrCh:
		// The server stopped on its own (e.g. bind failure surfaced async,
		// or something outside our control closed the listener).
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down pocketpress")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", slog.String("error", err.Error()))
		return err
	}

	// Shutdown closes the listener and waits for the Serve goroutine to
	// return, so this receive completes immediately; draining it keeps the
	// goroutine from leaking and lets us report a spurious serve error, if
	// any, that Shutdown itself wouldn't have surfaced.
	if err := <-serveErrCh; err != nil {
		return err
	}

	logger.Info("pocketpress stopped")
	return nil
}

// runBackground starts fn in its own goroutine on a child of ctx and returns a
// stop function that cancels that child and blocks until fn returns. Serve
// defers the stop functions so every supervised goroutine is cancelled and
// waited on before the store is closed, on every return path.
func runBackground(ctx context.Context, fn func(context.Context)) (stop func()) {
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		fn(runCtx)
		close(done)
	}()
	return func() {
		cancel()
		<-done
	}
}

// ResolveEncryptionKey returns the 32-byte AES key used to encrypt webhook
// secrets at rest. A non-empty PP_ENCRYPTION_KEY is accepted verbatim (hashed
// to 32 bytes); when empty, the key is derived from the resolved app secret
// instead, so a single configured PP_APP_SECRET is enough to keep webhook
// secrets decryptable across restarts. ephemeral is true only when the key was
// derived from an app secret that is itself ephemeral (no PP_ENCRYPTION_KEY and
// no PP_APP_SECRET) — the caller warns that stored webhook secrets will not
// survive a restart. Production fail-fast on a missing key arrives in Phase 8.
func ResolveEncryptionKey(configuredKey string, appSecret []byte, appSecretEphemeral bool) (key [32]byte, ephemeral bool) {
	if configuredKey != "" {
		return sha256.Sum256([]byte(configuredKey)), false
	}
	return sha256.Sum256(appSecret), appSecretEphemeral
}

// ResolvePreviewSecret returns the HMAC secret used to sign preview tokens.
// When configured (PP_APP_SECRET) it is used verbatim; when empty a random
// ephemeral secret is generated and ephemeral=true is returned so the caller
// can warn that preview tokens will not survive a restart. Production
// fail-fast on a missing secret arrives in Phase 8, so this never errors on
// an empty configured value — it only errors if the OS RNG fails.
func ResolvePreviewSecret(configured string) (secret []byte, ephemeral bool, err error) {
	if configured != "" {
		return []byte(configured), false, nil
	}
	buf := make([]byte, ephemeralSecretLen)
	if _, err := rand.Read(buf); err != nil {
		return nil, false, fmt.Errorf("generate ephemeral app secret: %w", err)
	}
	return buf, true, nil
}
