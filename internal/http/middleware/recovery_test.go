package middleware

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRecoveryTurnsPanicInto500Envelope confirms that a panicking handler
// wrapped in Recovery produces a 500 response in PocketPress's standard
// error envelope, instead of the connection dying with no response.
func TestRecoveryTurnsPanicInto500Envelope(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	panicking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	handler := Recovery(logger)(panicking)

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var got map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal response body %q: %v", rec.Body.String(), err)
	}

	errBody, ok := got["error"]
	if !ok {
		t.Fatalf("body = %v, missing top-level %q key", got, "error")
	}

	if errBody["code"] != "internal_error" {
		t.Fatalf("error.code = %v, want %q", errBody["code"], "internal_error")
	}
	if errBody["details"] != nil {
		t.Fatalf("error.details = %v, want nil", errBody["details"])
	}
}

// TestRecoveryPassesThroughNormalRequests confirms Recovery is a no-op
// when the wrapped handler doesn't panic.
func TestRecoveryPassesThroughNormalRequests(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("fine"))
	})

	handler := Recovery(logger)(ok)

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if rec.Body.String() != "fine" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "fine")
	}
}
