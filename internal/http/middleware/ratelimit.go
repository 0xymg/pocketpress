package middleware

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimiter is an in-memory, per-key sliding-window rate limiter. Each key
// (a client IP, in PocketPress's login limiter) is allowed at most `limit`
// events within any trailing `window`. It is safe for concurrent use.
//
// Memory is bounded without a background goroutine: every Allow opportunistically
// sweeps the whole map at most once per window, dropping keys whose events have
// all aged out, so idle attackers' buckets do not accumulate. Per key it retains
// at most `limit` timestamps.
type RateLimiter struct {
	mu        sync.Mutex
	attempts  map[string][]time.Time
	limit     int
	window    time.Duration
	lastSweep time.Time

	// now is the clock, overridable in tests to exercise window expiry
	// deterministically without sleeping. Defaults to time.Now.
	now func() time.Time
}

// NewRateLimiter builds a limiter allowing `limit` events per `window` per key.
// A non-positive limit is clamped to 1 and a non-positive window to one minute.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	if limit < 1 {
		limit = 1
	}
	if window <= 0 {
		window = time.Minute
	}
	return &RateLimiter{
		attempts: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
		now:      time.Now,
	}
}

// Allow records an event for key and reports whether it is within the limit.
// When the limit is exceeded it returns false along with the duration until the
// oldest in-window event ages out (the Retry-After hint); no event is recorded
// in that case, so a caller cannot push its own retry further into the future.
func (rl *RateLimiter) Allow(key string) (allowed bool, retryAfter time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	rl.sweepLocked(now)

	cutoff := now.Add(-rl.window)
	times := pruneBefore(rl.attempts[key], cutoff)

	if len(times) >= rl.limit {
		retryAfter = times[0].Add(rl.window).Sub(now)
		if retryAfter < 0 {
			retryAfter = 0
		}
		rl.attempts[key] = times
		return false, retryAfter
	}

	rl.attempts[key] = append(times, now)
	return true, 0
}

// Reset forgets all recorded events for key (e.g. after a successful login, so
// legitimate use never accrues toward the limit).
func (rl *RateLimiter) Reset(key string) {
	rl.mu.Lock()
	delete(rl.attempts, key)
	rl.mu.Unlock()
}

// sweepLocked drops fully-aged-out keys, at most once per window. The caller
// must hold rl.mu.
func (rl *RateLimiter) sweepLocked(now time.Time) {
	if !rl.lastSweep.IsZero() && now.Sub(rl.lastSweep) < rl.window {
		return
	}
	rl.lastSweep = now
	cutoff := now.Add(-rl.window)
	for key, times := range rl.attempts {
		pruned := pruneBefore(times, cutoff)
		if len(pruned) == 0 {
			delete(rl.attempts, key)
		} else {
			rl.attempts[key] = pruned
		}
	}
}

// pruneBefore returns the suffix of times (which is kept in ascending order)
// whose entries are strictly after cutoff, copying into a fresh slice only when
// something is dropped so a retained bucket keeps its backing array.
func pruneBefore(times []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(times) && !times[i].After(cutoff) {
		i++
	}
	if i == 0 {
		return times
	}
	remaining := times[i:]
	out := make([]time.Time, len(remaining))
	copy(out, remaining)
	return out
}

// ParseTrustedProxies parses PP_TRUSTED_PROXIES entries (bare IPs or CIDRs)
// into networks. A bare IP becomes a single-host network (/32 or /128).
// Unparseable entries are skipped.
func ParseTrustedProxies(entries []string) []*net.IPNet {
	var nets []*net.IPNet
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if _, ipnet, err := net.ParseCIDR(e); err == nil {
			nets = append(nets, ipnet)
			continue
		}
		if ip := net.ParseIP(e); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		}
	}
	return nets
}

// ClientIP resolves the client IP used for rate limiting. By default it is the
// direct peer (r.RemoteAddr host). X-Forwarded-For is honored only when the
// direct peer is one of the trusted proxies, in which case the real client is
// taken as the rightmost XFF entry that is not itself a trusted proxy (so a
// spoofed left-hand entry a client prepends cannot rotate the rate-limit key).
// A directly-connected client's XFF header is always ignored.
func ClientIP(r *http.Request, trusted []*net.IPNet) string {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	peer := net.ParseIP(host)
	if peer == nil || len(trusted) == 0 || !ipInNets(peer, trusted) {
		return host
	}

	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return host
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(parts[i])
		ip := net.ParseIP(candidate)
		if ip == nil {
			continue
		}
		if ipInNets(ip, trusted) {
			continue
		}
		return candidate
	}
	return host
}

func ipInNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// LoginRateLimit returns middleware that throttles POST /api/admin/v1/auth/login
// and POST /api/admin/v1/setup per client IP through limiter. Exceeding the
// limit returns the standard 429 "rate_limited" envelope with a Retry-After
// header; a 2xx response (a genuine login/setup) resets that IP's bucket. Every
// other request passes through untouched. A nil limiter disables throttling.
func LoginRateLimit(limiter *RateLimiter, trusted []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limiter == nil || !isRateLimitedAuthPath(r) {
				next.ServeHTTP(w, r)
				return
			}

			key := ClientIP(r, trusted)
			allowed, retryAfter := limiter.Allow(key)
			if !allowed {
				secs := int(math.Ceil(retryAfter.Seconds()))
				if secs < 1 {
					secs = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				WriteError(w, http.StatusTooManyRequests, "rate_limited", "Too many attempts. Try again shortly.")
				return
			}

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			if rec.status >= 200 && rec.status < 300 {
				limiter.Reset(key)
			}
		})
	}
}

// isRateLimitedAuthPath reports whether r targets one of the two rate-limited
// authentication endpoints.
func isRateLimitedAuthPath(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/admin/v1/auth/login", "/api/admin/v1/setup":
		return true
	default:
		return false
	}
}
