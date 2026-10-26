package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	dto "github.com/prometheus/client_model/go"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	collectorlog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"

	"booking/internal/pkg/telemetry/traces"
)

type metricCapture struct {
	collectormetric.UnimplementedMetricsServiceServer
	requests chan *collectormetric.ExportMetricsServiceRequest
}

func (c *metricCapture) Export(_ context.Context, request *collectormetric.ExportMetricsServiceRequest) (*collectormetric.ExportMetricsServiceResponse, error) {
	c.requests <- request
	return &collectormetric.ExportMetricsServiceResponse{}, nil
}

func TestGlobalPrometheusRegistryExportsThroughSharedOTLPConnection(t *testing.T) {
	preserveGlobals(t)
	previousRegisterer, previousGatherer := prometheus.DefaultRegisterer, prometheus.DefaultGatherer
	registry := prometheus.NewRegistry()
	prometheus.DefaultRegisterer, prometheus.DefaultGatherer = registry, registry
	t.Cleanup(func() {
		prometheus.DefaultRegisterer, prometheus.DefaultGatherer = previousRegisterer, previousGatherer
	})
	// Registration before Init must survive: SDK does not replace the global registry.
	counter := promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bridge_test_requests_total",
		Help: "Only a test fixture, never registered by a running application.",
	}, []string{"operation"})
	counter.WithLabelValues("create").Add(2)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	metrics := &metricCapture{requests: make(chan *collectormetric.ExportMetricsServiceRequest, 4)}
	logs := &logCapture{requests: make(chan *collectorlog.ExportLogsServiceRequest, 4)}
	spans := &traceCapture{requests: make(chan *collectortrace.ExportTraceServiceRequest, 4)}
	server := grpc.NewServer()
	collectormetric.RegisterMetricsServiceServer(server, metrics)
	collectorlog.RegisterLogsServiceServer(server, logs)
	collectortrace.RegisterTraceServiceServer(server, spans)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(server.Stop)
	t.Setenv("OTEL_BSP_SCHEDULE_DELAY", "60000")
	t.Setenv("OTEL_BLRP_SCHEDULE_DELAY", "60000")
	t.Setenv("OTEL_METRIC_EXPORT_TIMEOUT", "3000")
	telemetry, err := Init(context.Background(), Config{
		Enabled:         true,
		LogsEnabled:     true,
		MetricsEnabled:  true,
		MetricsInterval: time.Hour,
		ServiceName:     "bridge-test",
		OTLPEndpoint:    listener.Addr().String(),
		OTLPInsecure:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = telemetry.Shutdown(context.Background())
	})
	if prometheus.DefaultGatherer != registry || prometheus.DefaultRegisterer != registry {
		t.Fatal("Init replaced an existing global registry")
	}
	// Registration after Init must also be visible through the same gatherer.
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "bridge_test_pending",
		Help: "Test gauge.",
	})
	histogram := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "bridge_test_duration_seconds",
		Help:    "Test explicit buckets.",
		Buckets: []float64{0.1, 1},
	})
	prometheus.MustRegister(gauge, histogram)
	gauge.Set(7)
	histogram.Observe(0.05)
	histogram.Observe(0.25)
	provider, ok := telemetry.MeterProvider.(*sdkmetric.MeterProvider)
	if !ok {
		t.Fatal("enabled metrics must expose the SDK provider")
	}
	if err := provider.ForceFlush(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := receiveMetrics(t, metrics)
	checkBridgeBatch(t, first, 2, 7)

	counter.WithLabelValues("create").Add(3)
	gauge.Set(4)
	ctx, span := traces.Start(context.Background(), "shutdown-test")
	slog.WarnContext(ctx, "test final log")
	span.End()
	if err := telemetry.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	last := receiveMetrics(t, metrics)
	checkBridgeBatch(t, last, 5, 4)
	if telemetry.grpcConnection.GetState() != connectivity.Shutdown {
		t.Fatal("shared connection must close after all signals flush")
	}
	select {
	case <-logs.requests:
	default:
		t.Fatal("logs did not flush before connection close")
	}
	select {
	case <-spans.requests:
	default:
		t.Fatal("traces did not flush before connection close")
	}
	if err := telemetry.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func receiveMetrics(t *testing.T, capture *metricCapture) *collectormetric.ExportMetricsServiceRequest {
	t.Helper()
	select {
	case request := <-capture.requests:
		return request
	case <-time.After(5 * time.Second):
		t.Fatal("OTLP metric export did not arrive")
		return nil
	}
}

func checkBridgeBatch(t *testing.T, request *collectormetric.ExportMetricsServiceRequest, count, gauge float64) {
	t.Helper()
	if len(request.ResourceMetrics) != 1 {
		t.Fatalf("resource batches: %d", len(request.ResourceMetrics))
	}
	resource := request.ResourceMetrics[0]
	attrs := stringAttributes(resource.Resource.Attributes)
	if attrs["service.name"] != "bridge-test" || attrs["service.instance.id"] == "" {
		t.Fatalf("lost process identity: %v", attrs)
	}
	all := make(map[string]*metricpb.Metric)
	for _, scope := range resource.ScopeMetrics {
		for _, m := range scope.Metrics {
			all[m.Name] = m
		}
	}
	if len(all) != 3 {
		t.Fatalf("SDK unexpectedly registered instruments: %v", all)
	}
	sum := all["bridge_test_requests_total"].GetSum()
	if sum == nil || !sum.IsMonotonic || sum.AggregationTemporality != metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE {
		t.Fatalf("counter is not cumulative: %v", sum)
	}
	if len(sum.DataPoints) != 1 || sum.DataPoints[0].GetAsDouble() != count || stringAttributes(sum.DataPoints[0].Attributes)["operation"] != "create" {
		t.Fatalf("counter value or labels lost: %v", sum)
	}
	if got := all["bridge_test_pending"].GetGauge().DataPoints[0].GetAsDouble(); got != gauge {
		t.Fatalf("gauge = %v, want %v", got, gauge)
	}
	hist := all["bridge_test_duration_seconds"].GetHistogram()
	point := hist.DataPoints[0]
	if point.Count != 2 || math.Abs(point.GetSum()-0.3) > 1e-9 || len(point.ExplicitBounds) != 2 || point.ExplicitBounds[0] != 0.1 || point.ExplicitBounds[1] != 1 {
		t.Fatalf("histogram bounds/count/sum changed: %v", point)
	}
	if len(point.BucketCounts) != 3 || point.BucketCounts[0] != 1 || point.BucketCounts[1] != 1 || point.BucketCounts[2] != 0 {
		t.Fatalf("histogram bucket counts changed: %v", point.BucketCounts)
	}
}

func TestMetricsDisabledDoesNotGatherGlobalRegistry(t *testing.T) {
	preserveGlobals(t)
	previous := prometheus.DefaultGatherer
	calls := 0
	prometheus.DefaultGatherer = prometheus.GathererFunc(func() ([]*dto.MetricFamily, error) {
		calls++
		return nil, errors.New("must not collect while disabled")
	})
	t.Cleanup(func() {
		prometheus.DefaultGatherer = previous
	})
	for _, enabled := range []bool{false, true} {
		telemetry, err := Init(context.Background(), Config{
			Enabled:        enabled,
			MetricsEnabled: false,
			ServiceName:    "disabled-test",
			OTLPEndpoint:   "127.0.0.1:1",
			OTLPInsecure:   true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := telemetry.MeterProvider.(*sdkmetric.MeterProvider); ok {
			t.Fatal("disabled metrics initialized an SDK provider")
		}
		if err := telemetry.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 0 {
		t.Fatalf("disabled SDK gathered metrics %d times", calls)
	}
}
