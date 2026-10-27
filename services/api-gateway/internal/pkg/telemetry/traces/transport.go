package traces

import (
	"context"
	"net/http"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/stats"
)

type httpRouteContextKey struct{}

type httpRoute struct {
	pattern string
}

// HTTP instruments the real HTTP listener, including Clay's local HTTP adapter.
func HTTP(next http.Handler) http.Handler {
	handler := otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The server span already exists here. Set the header before any handler
		// can write a response, including routing, validation and downstream errors.
		if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
			w.Header().Set("X-Trace-Id", sc.TraceID().String())
		}
		next.ServeHTTP(w, r)
	}), "HTTP",
		otelhttp.WithPropagators(propagation.TraceContext{}),
		otelhttp.WithMeterProvider(metricnoop.NewMeterProvider()),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			if route, ok := r.Context().Value(httpRouteContextKey{}).(*httpRoute); ok && route.pattern != "" {
				return r.Method + " " + route.pattern
			}
			return r.Method
		}),
		otelhttp.WithFilter(func(r *http.Request) bool {
			return !strings.HasPrefix(r.URL.Path, "/healthcheck/") &&
				!strings.HasPrefix(r.URL.Path, "/debug") &&
				!strings.HasPrefix(r.URL.Path, "/swagger") &&
				!strings.HasPrefix(r.URL.Path, "/docs")
		}),
	)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// grpc-gateway and chi pass request copies. A request-local holder keeps
		// the matched route available when otelhttp formats the span again after
		// chi has populated Request.Pattern with the outer mount pattern.
		route := &httpRoute{}
		ctx := context.WithValue(r.Context(), httpRouteContextKey{}, route)
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AnnotateHTTPRoute runs after grpc-gateway has matched a route, before any
// handler span. It updates the HTTP server span without creating a fake gRPC hop.
func AnnotateHTTPRoute(next runtime.HandlerFunc) runtime.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, pathParams map[string]string) {
		if pattern, ok := runtime.HTTPPattern(r.Context()); ok {
			if route, ok := r.Context().Value(httpRouteContextKey{}).(*httpRoute); ok {
				route.pattern = pattern.String()
			}
			span := trace.SpanFromContext(r.Context())
			span.SetName(r.Method + " " + pattern.String())
			span.SetAttributes(attribute.String("http.route", pattern.String()))
		}
		next(w, r, pathParams)
	}
}

func ServerHandler() stats.Handler {
	return otelgrpc.NewServerHandler(
		otelgrpc.WithPropagators(propagation.TraceContext{}),
		otelgrpc.WithMeterProvider(metricnoop.NewMeterProvider()),
		otelgrpc.WithFilter(traceRPC),
	)
}

func ClientHandler() stats.Handler {
	return otelgrpc.NewClientHandler(
		otelgrpc.WithPropagators(propagation.TraceContext{}),
		otelgrpc.WithMeterProvider(metricnoop.NewMeterProvider()),
		otelgrpc.WithFilter(traceRPC),
	)
}

func traceRPC(info *stats.RPCTagInfo) bool {
	return !strings.HasPrefix(info.FullMethodName, "/grpc.health.v1.Health/") &&
		!strings.HasPrefix(info.FullMethodName, "/grpc.reflection.")
}
