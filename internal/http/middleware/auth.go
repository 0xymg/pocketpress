package middleware

import (
	"context"
	"net/http"

	"github.com/pocketpress/pocketpress/internal/store"
)

// SessionCookieName is the name of the HTTP-only cookie that carries the
// raw session token. Both the admin API (which sets and clears it) and the
// RequireUser middleware (which reads it) refer to this single constant.
const SessionCookieName = "pp_session"

// contextKey is an unexported type for context keys defined in this
// package, so they never collide with keys from other packages.
type contextKey int

const userContextKey contextKey = iota

// UserResolver resolves a raw session token to the user it authenticates.
// It is implemented by *auth.Service; the middleware depends on this narrow
// interface rather than the concrete service so it stays decoupled from the
// auth package.
type UserResolver interface {
	UserForSessionToken(ctx context.Context, token string) (store.User, error)
}

// UserFromContext returns the authenticated user placed in ctx by
// RequireUser, and whether one was present.
func UserFromContext(ctx context.Context) (store.User, bool) {
	u, ok := ctx.Value(userContextKey).(store.User)
	return u, ok
}

// RequireUser builds middleware that authenticates the request from the
// session cookie. On success it stores the resolved user in the request
// context (retrievable with UserFromContext) and calls next; otherwise it
// writes a 401 "unauthorized" envelope and stops. Any resolver error —
// missing cookie, unknown/expired session, inactive user — yields the same
// generic 401 so nothing leaks about why authentication failed.
func RequireUser(resolver UserResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(SessionCookieName)
			if err != nil || cookie.Value == "" {
				WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
				return
			}

			user, err := resolver.UserForSessionToken(r.Context(), cookie.Value)
			if err != nil {
				WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireOwner builds middleware that allows only owner-role users through.
// It must run after RequireUser (it reads the user RequireUser stored in
// the context). A non-owner gets a 403 "forbidden" envelope; a missing user
// (RequireUser not run) gets a 401.
func RequireOwner() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
				return
			}
			if user.Role != store.RoleOwner {
				WriteError(w, http.StatusForbidden, "forbidden", "This action requires the owner role.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
