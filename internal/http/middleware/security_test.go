package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func serveGET(h http.Handler, path string, tlsState *tls.ConnectionState) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.TLS = tlsState
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestSecurityHeadersCommonAndCSPScope(t *testing.T) {
	h := SecurityHeaders(SecurityConfig{SPACSP: "default-src 'self'"})(okHandler())

	// API path: common headers present, CSP absent.
	api := serveGET(h, "/api/admin/v1/posts", nil)
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	} {
		if got := api.Header().Get(k); got != want {
			t.Fatalf("API %s = %q, want %q", k, got, want)
		}
	}
	if csp := api.Header().Get("Content-Security-Policy"); csp != "" {
		t.Fatalf("API path got a CSP header %q, want none", csp)
	}

	// SPA path: CSP present.
	spa := serveGET(h, "/posts/new", nil)
	if csp := spa.Header().Get("Content-Security-Policy"); csp != "default-src 'self'" {
		t.Fatalf("SPA CSP = %q, want the configured policy", csp)
	}
}

func TestSecurityHeadersHSTS(t *testing.T) {
	cases := []struct {
		name       string
		cfg        SecurityConfig
		tlsState   *tls.ConnectionState
		wantHeader string
	}{
		{"non-production never sets HSTS", SecurityConfig{Production: false, PublicURLHTTPS: true}, &tls.ConnectionState{}, ""},
		{"production over direct TLS", SecurityConfig{Production: true}, &tls.ConnectionState{}, hstsValue},
		{"production behind https proxy", SecurityConfig{Production: true, PublicURLHTTPS: true}, nil, hstsValue},
		{"production plain http no https url", SecurityConfig{Production: true, PublicURLHTTPS: false}, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := SecurityHeaders(tc.cfg)(okHandler())
			rec := serveGET(h, "/", tc.tlsState)
			if got := rec.Header().Get("Strict-Transport-Security"); got != tc.wantHeader {
				t.Fatalf("HSTS = %q, want %q", got, tc.wantHeader)
			}
		})
	}
}
