package metrics

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	prombridge "go.opentelemetry.io/contrib/bridges/prometheus"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/grpc"
)

// NewProvider exports the global Prometheus registry over the shared OTLP connection.
// Instruments are registered by application code through prometheus or promauto.
func NewProvider(ctx context.Context, res *resource.Resource, connection *grpc.ClientConn, interval time.Duration) (*sdkmetric.MeterProvider, error) {
	exporter, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithGRPCConn(connection))
	if err != nil {
		return nil, fmt.Errorf("telemetry: create OTLP metric exporter: %w", err)
	}
	bridge := prombridge.NewMetricProducer(prombridge.WithGatherer(prometheus.DefaultGatherer))
	reader := sdkmetric.NewPeriodicReader(exporter,
		sdkmetric.WithInterval(interval),
		sdkmetric.WithProducer(bridge),
	)
	return sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(reader),
	), nil
}
