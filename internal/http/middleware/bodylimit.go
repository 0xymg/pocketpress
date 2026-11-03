package middleware

import "net/http"

// DefaultMaxBodyBytes is the request body cap applied to every route that does
// not opt into a larger limit: 1 MiB. It is generous for JSON admin payloads
// while bounding what a single request can stream into the process.
const DefaultMaxBodyBytes int64 = 1 << 20

// BodyLimit returns middleware that installs a request-body cap on every
// request by wrapping r.Body in http.MaxBytesReader. The cap for a given
// request is chosen by limitFor; when limitFor is nil (or returns a
// non-positive value) DefaultMaxBodyBytes is used.
//
// This is a defence-in-depth backstop: individual handlers still install their
// own, usually tighter, caps (the admin JSON decode path at 1 MiB, the post
// endpoints at 5 MiB, media upload at PP_MAX_UPLOAD_MB). Because MaxBytesReader
// produces a *http.MaxBytesError that those handlers already translate into the
// standard JSON envelope, nesting the middleware cap outside a handler cap is
// safe as long as the middleware cap is not smaller than the handler's — which
// is why limitFor grants the post and media paths their larger allowances
// rather than clamping everything to 1 MiB. Handlers that never read their
// body are unaffected; the wrapped reader simply goes unused.
func BodyLimit(limitFor func(*http.Request) int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limit := DefaultMaxBodyBytes
			if limitFor != nil {
				if n := limitFor(r); n > 0 {
					limit = n
				}
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}
