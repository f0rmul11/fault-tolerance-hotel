package requesttimeout

import (
	"context"
	"net/http"
	"time"
)

// MaxDuration is the total processing budget at the gateway, including downstream calls.
const MaxDuration = 5 * time.Second

// Middleware starts the HTTP budget before decoding the request body.
// The optional client timeout may shorten the budget, but cannot extend it.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		duration := MaxDuration
		if requested, err := time.ParseDuration(r.Header.Get("X-Request-Timeout")); err == nil && requested > 0 && requested < duration {
			duration = requested
		}

		ctx, cancel := context.WithTimeout(r.Context(), duration)
		defer cancel()

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
