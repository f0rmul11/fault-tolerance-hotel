package logs

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/grpc"
)

// NewProvider borrows Telemetry's connection and uses the SDK's bounded batch queue.
func NewProvider(ctx context.Context, res *resource.Resource, connection *grpc.ClientConn) (*sdklog.LoggerProvider, error) {
	exporter, err := otlploggrpc.New(ctx, otlploggrpc.WithGRPCConn(connection))
	if err != nil {
		return nil, fmt.Errorf("telemetry: create OTLP log exporter: %w", err)
	}
	return sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	), nil
}
