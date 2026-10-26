// Package traces exposes layer spans and W3C context propagation.
package traces

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

var tracerName atomic.Value

// SetTracerName configures the instrumentation scope once during process startup.
func SetTracerName(name string) {
	tracerName.Store(name)
}

func Start(ctx context.Context, name string, options ...trace.SpanStartOption) (context.Context, trace.Span) {
	scope := "hotelflow"
	if configured := tracerName.Load(); configured != nil {
		scope = configured.(string)
	}
	return otel.Tracer(scope).Start(ctx, name, options...)
}

// Inject preserves business headers and adds only W3C trace context.
func Inject(ctx context.Context, headers map[string]string) {
	if headers == nil {
		return
	}
	propagation.TraceContext{}.Inject(ctx, propagation.MapCarrier(headers))
}

// Extract preserves the caller's cancellation, deadlines and transaction values.
func Extract(ctx context.Context, headers map[string]string) context.Context {
	return propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier(headers))
}

func RecordError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, "operation failed")
}
