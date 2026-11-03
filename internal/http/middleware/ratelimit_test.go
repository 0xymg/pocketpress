package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterAllowsUpToLimitThenDenies(t *testing.T) {
	fixed := time.Now()
	rl := NewRateLimiter(5, time.Minute)
	rl.now = func() time.Time { return fixed }

	for i := 1; i <= 5; i++ {
		if ok, _ := rl.Allow("k"); !ok {
			t.Fatalf("attempt %d denied, want allowed within limit", i)
		}
	}
	ok, retryAfter := rl.Allow("k")
	if ok {
		t.Fatal("6th attempt allowed, want denied")
	}
	if retryAfter <= 0 || retryAfter > time.Minute {
		t.Fatalf("retryAfter = %v, want (0, 1m]", retryAfter)
	}
}

func TestRateLimiterKeysIndependent(t *testing.T) {
	fixed := time.Now()
	rl := NewRateLimiter(2, time.Minute)
	rl.now = func() time.Time { return fixed }

	rl.Allow("a")
	rl.Allow("a")
	if ok, _ := rl.Allow("a"); ok {
		t.Fatal("a exceeded its limit but was allowed")
	}
	if ok, _ := rl.Allow("b"); !ok {
		t.Fatal("b was denied — keys are not independent")
	}
}

func TestRateLimiterWindowExpiryRestores(t *testing.T) {
	clock := time.Now()
	rl := NewRateLimiter(1, time.Minute)
	rl.now = func() time.Time { return clock }

	if ok, _ := rl.Allow("k"); !ok {
		t.Fatal("first attempt denied")
	}
	if ok, _ := rl.Allow("k"); ok {
		t.Fatal("second attempt within window allowed")
	}

	clock = clock.Add(61 * time.Second) // advance past the window
	if ok, _ := rl.Allow("k"); !ok {
		t.Fatal("attempt after the window expired was denied, want restored")
	}
}

func TestRateLimiterReset(t *testing.T) {
	fixed := time.Now()
	rl := NewRateLimiter(1, time.Minute)
	rl.now = func() time.Time { return fixed }

	rl.Allow("k")
	if ok, _ := rl.Allow("k"); ok {
		t.Fatal("second attempt allowed before reset")
	}
	rl.Reset("k")
	if ok, _ := rl.Allow("k"); !ok {
		t.Fatal("attempt after reset denied, want allowed")
	}
}

func TestParseTrustedProxies(t *testing.T) {
	nets := ParseTrustedProxies([]string{"10.0.0.1", "192.168.0.0/16", "garbage", "", " 172.16.0.1 "})
	if len(nets) != 3 {
		t.Fatalf("parsed %d networks, want 3", len(nets))
	}
	if !ipInNets(net.ParseIP("10.0.0.1"), nets) {
		t.Fatal("10.0.0.1 (bare IP) not matched")
	}
	if !ipInNets(net.ParseIP("192.168.5.5"), nets) {
		t.Fatal("192.168.5.5 (in CIDR) not matched")
	}
	if ipInNets(net.ParseIP("203.0.113.9"), nets) {
		t.Fatal("203.0.113.9 unexpectedly matched a trusted network")
	}
}

func TestClientIPDefaultsToDirectPeerIgnoringXFF(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "203.0.113.9:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	// No trusted proxies: XFF must be ignored entirely.
	if got := ClientIP(r, nil); got != "203.0.113.9" {
		t.Fatalf("ClientIP(no trusted) = %q, want direct peer 203.0.113.9", got)
	}
}

func TestClientIPUntrustedPeerIgnoresSpoofedXFF(t *testing.T) {
	trusted := ParseTrustedProxies([]string{"10.0.0.0/8"})
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "203.0.113.9:1234" // not within 10.0.0.0/8
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	if got := ClientIP(r, trusted); got != "203.0.113.9" {
		t.Fatalf("ClientIP(untrusted peer) = %q, want direct peer (spoofed XFF ignored)", got)
	}
}

func TestClientIPTrustedPeerHonorsXFF(t *testing.T) {
	trusted := ParseTrustedProxies([]string{"10.0.0.1"})
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "10.0.0.1:9999"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	if got := ClientIP(r, trusted); got != "1.2.3.4" {
		t.Fatalf("ClientIP(trusted peer) = %q, want the forwarded client 1.2.3.4", got)
	}
}

func TestClientIPTrustedPeerSkipsChainedProxies(t *testing.T) {
	trusted := ParseTrustedProxies([]string{"10.0.0.0/8"})
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "10.0.0.2:9999"
	// Real client, then two internal proxies the chain appended on the right.
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.9, 10.0.0.2")

	if got := ClientIP(r, trusted); got != "1.2.3.4" {
		t.Fatalf("ClientIP(chained) = %q, want the rightmost untrusted 1.2.3.4", got)
	}
}

// loginPost issues a POST to the login endpoint from the given peer through h.
func loginPost(h http.Handler, peer string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", nil)
	r.RemoteAddr = peer
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestLoginRateLimitMiddlewareThrottlesWith429(t *testing.T) {
	limiter := NewRateLimiter(2, time.Minute)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized) // simulate failed logins
	})
	h := LoginRateLimit(limiter, nil)(next)

	loginPost(h, "203.0.113.5:1111")
	loginPost(h, "203.0.113.5:2222") // same IP, different source port
	rec := loginPost(h, "203.0.113.5:3333")

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd attempt status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 response is missing the Retry-After header")
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details any    `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode 429 body: %v", err)
	}
	if env.Error.Code != "rate_limited" {
		t.Fatalf("error.code = %q, want rate_limited", env.Error.Code)
	}
	if env.Error.Details != nil {
		t.Fatalf("error.details = %v, want null", env.Error.Details)
	}
}

func TestLoginRateLimitDifferentIPsIndependent(t *testing.T) {
	limiter := NewRateLimiter(1, time.Minute)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	h := LoginRateLimit(limiter, nil)(next)

	if rec := loginPost(h, "198.51.100.1:1000"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("IP A first attempt = %d, want 401", rec.Code)
	}
	if rec := loginPost(h, "198.51.100.1:1001"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("IP A second attempt = %d, want 429", rec.Code)
	}
	if rec := loginPost(h, "198.51.100.2:1000"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("IP B first attempt = %d, want 401 (independent from IP A)", rec.Code)
	}
}

func TestLoginRateLimitResetsOnSuccess(t *testing.T) {
	limiter := NewRateLimiter(2, time.Minute)
	status := http.StatusUnauthorized
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	})
	h := LoginRateLimit(limiter, nil)(next)
	const peer = "198.51.100.7:5000"

	loginPost(h, peer) // one failure (count = 1)

	status = http.StatusOK
	loginPost(h, peer) // success clears the bucket

	status = http.StatusUnauthorized
	if rec := loginPost(h, peer); rec.Code != http.StatusUnauthorized {
		t.Fatalf("post-reset attempt 1 = %d, want 401", rec.Code)
	}
	if rec := loginPost(h, peer); rec.Code != http.StatusUnauthorized {
		t.Fatalf("post-reset attempt 2 = %d, want 401", rec.Code)
	}
	if rec := loginPost(h, peer); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("post-reset attempt 3 = %d, want 429", rec.Code)
	}
}

func TestLoginRateLimitIgnoresNonAuthPaths(t *testing.T) {
	limiter := NewRateLimiter(1, time.Minute)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := LoginRateLimit(limiter, nil)(next)

	for i := 0; i < 10; i++ {
		r := httptest.NewRequest(http.MethodGet, "/api/admin/v1/posts", nil)
		r.RemoteAddr = "198.51.100.9:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("non-auth request %d throttled (status %d), want passthrough", i, rec.Code)
		}
	}
}
