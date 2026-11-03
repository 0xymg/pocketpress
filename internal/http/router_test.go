package http

import (
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketpress/pocketpress/admin"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestHealthzExactBody locks down the fixed /healthz response contract:
// {"status":"ok","version":"<version>"}.
func TestHealthzExactBody(t *testing.T) {
	handler := NewRouter(Config{Logger: discardLogger(), Version: "0.1.0-test"})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json prefix", ct)
	}

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal response body %q: %v", rec.Body.String(), err)
	}

	want := map[string]string{"status": "ok", "version": "0.1.0-test"}
	if len(got) != len(want) || got["status"] != want["status"] || got["version"] != want["version"] {
		t.Fatalf("body = %v, want %v", got, want)
	}
}

// TestSPAFallbackServesIndexForClientRoute confirms that a non-file GET
// path such as /posts/new falls back to the SPA's index.html rather than
// 404ing, so client-side routing works.
func TestSPAFallbackServesIndexForClientRoute(t *testing.T) {
	handler := NewRouter(Config{Logger: discardLogger(), Version: "0.1.0-test"})

	indexReq := httptest.NewRequest(http.MethodGet, "/", nil)
	indexRec := httptest.NewRecorder()
	handler.ServeHTTP(indexRec, indexReq)

	if indexRec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", indexRec.Code, http.StatusOK)
	}

	fallbackReq := httptest.NewRequest(http.MethodGet, "/posts/new", nil)
	fallbackRec := httptest.NewRecorder()
	handler.ServeHTTP(fallbackRec, fallbackReq)

	if fallbackRec.Code != http.StatusOK {
		t.Fatalf("GET /posts/new status = %d, want %d", fallbackRec.Code, http.StatusOK)
	}

	if fallbackRec.Body.String() != indexRec.Body.String() {
		t.Fatalf("GET /posts/new body did not match GET / (index.html) body")
	}

	if !strings.Contains(fallbackRec.Body.String(), "PocketPress") {
		t.Fatalf("GET /posts/new body = %q, want it to contain %q", fallbackRec.Body.String(), "PocketPress")
	}
}

// TestSPAServesStaticAssetContentType confirms static files are served
// directly (not the SPA fallback) with a content type derived from their
// extension. This test is robust to build layout changes: it walks the
// embedded admin dist filesystem to find the first .css file dynamically
// rather than hardcoding a path.
func TestSPAServesStaticAssetContentType(t *testing.T) {
	handler := NewRouter(Config{Logger: discardLogger(), Version: "0.1.0-test"})

	// Find the first CSS file in the embedded admin dist filesystem.
	distFS, err := admin.FS()
	if err != nil {
		t.Fatalf("failed to get admin FS: %v", err)
	}

	var cssPath string
	err = fs.WalkDir(distFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if cssPath == "" && !d.IsDir() && strings.HasSuffix(path, ".css") {
			cssPath = path
		}
		return nil
	})

	if err != nil {
		t.Fatalf("error walking dist FS: %v", err)
	}

	if cssPath == "" {
		t.Skip("no .css file found in embedded admin dist")
	}

	// Request the CSS file with a leading slash.
	req := httptest.NewRequest(http.MethodGet, "/"+cssPath, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Fatalf("Content-Type = %q, want text/css prefix", ct)
	}
}

// TestAPIUnknownReturns404Envelope confirms /api/* paths that aren't
// (yet) implemented return PocketPress's standard 404 JSON envelope
// instead of falling through to the SPA.
func TestAPIUnknownReturns404Envelope(t *testing.T) {
	handler := NewRouter(Config{Logger: discardLogger(), Version: "0.1.0-test"})

	req := httptest.NewRequest(http.MethodGet, "/api/unknown", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	var got map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal response body %q: %v", rec.Body.String(), err)
	}

	errBody, ok := got["error"]
	if !ok {
		t.Fatalf("body = %v, missing top-level %q key", got, "error")
	}

	if errBody["code"] != "not_found" {
		t.Fatalf("error.code = %v, want %q", errBody["code"], "not_found")
	}
	if errBody["message"] != "Not found." {
		t.Fatalf("error.message = %v, want %q", errBody["message"], "Not found.")
	}
	if errBody["details"] != nil {
		t.Fatalf("error.details = %v, want nil", errBody["details"])
	}
}
