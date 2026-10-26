package traces

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc"
)

// NewProvider borrows the process-wide connection. Its owner must flush this
// provider before closing the connection. The SDK reads OTEL_TRACES_SAMPLER and
// OTEL_BSP_*; queues stay bounded and never block business requests on enqueue.
func NewProvider(ctx context.Context, res *resource.Resource, connection *grpc.ClientConn) (*sdktrace.TracerProvider, error) {
	if connection == nil {
		return nil, fmt.Errorf("traces: OTLP connection не должна быть nil")
	}
	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(connection))
	if err != nil {
		return nil, fmt.Errorf("traces: create OTLP exporter: %w", err)
	}
	return sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exporter),
	), nil
}
