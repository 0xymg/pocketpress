package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRequestLoggerLogsMethodPathStatus confirms RequestLogger records the
// method, path, and resulting status code of the wrapped handler.
func TestRequestLoggerLogsMethodPathStatus(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	handler := RequestLogger(logger)(next)

	req := httptest.NewRequest(http.MethodPost, "/widgets", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	logOutput := buf.String()
	for _, want := range []string{"method=POST", "path=/widgets", "status=201"} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("log output = %q, want it to contain %q", logOutput, want)
		}
	}
}
