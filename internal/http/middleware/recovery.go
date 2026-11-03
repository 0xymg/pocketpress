package middleware

import (
	"log/slog"
	"net/http"
)

// Recovery returns middleware that recovers from panics raised by the
// wrapped handler, logs the panic value, and responds with a 500 in
// PocketPress's standard error envelope instead of letting the connection
// die with no response.
func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered",
						slog.Any("panic", rec),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
					)
					WriteError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
