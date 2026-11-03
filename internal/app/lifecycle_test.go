package app

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// safeBuffer is a concurrency-safe io.Writer for capturing slog output while
// Serve writes to it from its own goroutines.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// extractHTTPAddr pulls the http_addr=<value> field out of the "starting
// pocketpress" log line Serve emits once the listener is bound.
func extractHTTPAddr(logs string) string {
	const key = "http_addr="
	i := strings.Index(logs, key)
	if i < 0 {
		return ""
	}
	rest := logs[i+len(key):]
	end := strings.IndexAny(rest, " \t\r\n")
	if end < 0 {
		end = len(rest)
	}
	return strings.Trim(rest[:end], `"`)
}

// TestServeStartsAndShutsDownGracefully drives Serve end to end against a
// real (random-port) listener: it confirms that once the server is actually
// accepting connections, cancelling the context makes Serve return cleanly
// instead of hanging or erroring.
//
// Readiness is established by waiting until the server accepts an HTTP request
// (not merely until the data dir appears). Because Serve binds the listener
// only after migrations complete, a successful request proves migrations are
// done — so cancelling afterward can never race an in-flight migration and
// abort it with "context canceled", which is what previously made this test
// flaky under -race.
func TestServeStartsAndShutsDownGracefully(t *testing.T) {
	cfg := Config{
		Env:      "development",
		HTTPAddr: "127.0.0.1:0",
		DataDir:  filepath.Join(t.TempDir(), "pp_data"),
	}
	logBuf := &safeBuffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, nil))

	build := func(*sql.DB) Components {
		return Components{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})}
	}

	ctx, cancel := context.WithCancel(context.Background())

	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- Serve(ctx, cfg, logger, build)
	}()

	// Wait for the listen address to appear in the logs (emitted after the
	// listener is bound, which is after migrations run).
	var addr string
	addrDeadline := time.Now().Add(5 * time.Second)
	for {
		if a := extractHTTPAddr(logBuf.String()); a != "" {
			addr = a
			break
		}
		if time.Now().After(addrDeadline) {
			t.Fatalf("server did not log its listen address within timeout; logs:\n%s", logBuf.String())
		}
		if err := earlyServeError(serveErrCh); err != nil {
			t.Fatalf("Serve returned early with error: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Confirm the server actually accepts a request before triggering shutdown.
	readyDeadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get("http://" + addr + "/")
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		if time.Now().After(readyDeadline) {
			t.Fatalf("server at %s never accepted a request: %v", addr, err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()

	select {
	case err := <-serveErrCh:
		if err != nil {
			t.Fatalf("Serve returned error after shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return within 5s of context cancellation")
	}
}

// earlyServeError non-blockingly reports whether Serve has already returned an
// error (e.g. a startup failure), so the readiness loop fails fast instead of
// spinning until its deadline.
func earlyServeError(ch <-chan error) error {
	select {
	case err := <-ch:
		return err
	default:
		return nil
	}
}

// TestServeReturnsErrorOnListenFailure confirms Serve surfaces a listen
// failure (e.g. an already-in-use or otherwise unbindable address) instead
// of hanging.
func TestServeReturnsErrorOnListenFailure(t *testing.T) {
	cfg := Config{
		Env: "development",
		// Port 0 lets the OS pick a free port; binding to an invalid host
		// deterministically fails instead.
		HTTPAddr: "127.0.0.1:-1",
		DataDir:  filepath.Join(t.TempDir(), "pp_data"),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	build := func(*sql.DB) Components {
		return Components{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})}
	}

	err := Serve(context.Background(), cfg, logger, build)
	if err == nil {
		t.Fatal("Serve returned nil error, want a listen error for an invalid address")
	}
}
