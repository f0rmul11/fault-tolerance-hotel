package metrics

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
)

type routeKey struct{}
type route struct{ pattern string }

// HTTP measures the real listener, including local grpc-gateway adapters.
func HTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/healthcheck/") || strings.HasPrefix(r.URL.Path, "/debug") ||
			strings.HasPrefix(r.URL.Path, "/swagger") || strings.HasPrefix(r.URL.Path, "/docs") {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		matched := &route{pattern: "unmatched"}
		writer := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		defer func() {
			failure := recover()
			code := writer.Status()
			if code == 0 {
				code = http.StatusOK
				if failure != nil {
					code = http.StatusInternalServerError
				} else if r.Context().Err() == context.Canceled {
					code = 499
				}
			}
			handler := httpMethod(r.Method) + " " + matched.pattern
			initHTTPSeries(handler)
			serverRequests.WithLabelValues("http", handler, httpCategory(code), strconv.Itoa(code)).Inc()
			serverDuration.WithLabelValues("http", handler, httpCategory(code)).Observe(time.Since(started).Seconds())
			if failure != nil {
				panic(failure)
			}
		}()
		next.ServeHTTP(writer, r.WithContext(context.WithValue(r.Context(), routeKey{}, matched)))
	})
}

// AnnotateHTTPRoute keeps the route template across request copies made by chi
// and grpc-gateway. IDs, query parameters and raw URLs never become labels.
func AnnotateHTTPRoute(next runtime.HandlerFunc) runtime.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, params map[string]string) {
		if pattern, ok := runtime.HTTPPattern(r.Context()); ok {
			if matched, ok := r.Context().Value(routeKey{}).(*route); ok {
				matched.pattern = pattern.String()
				initHTTPSeries(httpMethod(r.Method) + " " + matched.pattern)
			}
		}
		next(w, r, params)
	}
}

func initHTTPSeries(handler string) {
	// Statuses emitted by the adapter and its error mapping. Other HTTP statuses
	// are still recorded, but do not allocate hundreds of unused series per route.
	for _, code := range []int{200, 400, 401, 403, 404, 405, 409, 422, 429, 499, 500, 501, 503, 504} {
		serverRequests.WithLabelValues("http", handler, httpCategory(code), strconv.Itoa(code))
	}
}

func httpMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return method
	default:
		return "OTHER"
	}
}
