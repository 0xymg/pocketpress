package middleware

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// readBodyHandler reads the whole request body and stashes any error, so a test
// can observe whether the middleware's cap tripped.
func readBodyHandler(readErr *error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		*readErr = err
	})
}

func TestBodyLimitInstallsDefaultCap(t *testing.T) {
	var readErr error
	h := BodyLimit(nil)(readBodyHandler(&readErr)) // nil limitFor -> default 1 MiB

	body := bytes.NewReader(make([]byte, DefaultMaxBodyBytes+1))
	r := httptest.NewRequest(http.MethodPost, "/anything", body)
	h.ServeHTTP(httptest.NewRecorder(), r)

	var maxErr *http.MaxBytesError
	if !errors.As(readErr, &maxErr) {
		t.Fatalf("read error = %v, want *http.MaxBytesError from the middleware-installed cap", readErr)
	}
}

func TestBodyLimitHonorsLargerLimit(t *testing.T) {
	var readErr error
	h := BodyLimit(func(r *http.Request) int64 { return 4 << 20 })(readBodyHandler(&readErr))

	body := bytes.NewReader(make([]byte, 2<<20)) // 2 MiB, under the 4 MiB cap
	r := httptest.NewRequest(http.MethodPost, "/posts", body)
	h.ServeHTTP(httptest.NewRecorder(), r)

	if readErr != nil {
		t.Fatalf("read error = %v, want nil for a body under the larger cap", readErr)
	}
}
