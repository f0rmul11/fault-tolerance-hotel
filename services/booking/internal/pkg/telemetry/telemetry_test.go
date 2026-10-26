package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"

	"booking/internal/pkg/telemetry/traces"
)

func preserveGlobals(t *testing.T) {
	t.Helper()
	provider := otel.GetTracerProvider()
	meterProvider := otel.GetMeterProvider()
	loggerProvider := otel.GetLoggerProvider()
	logger := slog.Default()
	propagator := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(provider)
		otel.SetMeterProvider(meterProvider)
		otel.SetLoggerProvider(loggerProvider)
		slog.SetDefault(logger)
		otel.SetTextMapPropagator(propagator)
	})
}

type traceCapture struct {
	collectortrace.UnimplementedTraceServiceServer
	requests chan *collectortrace.ExportTraceServiceRequest
}

func (c *traceCapture) Export(_ context.Context, request *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	c.requests <- request
	return &collectortrace.ExportTraceServiceResponse{}, nil
}

func TestInitExportsQueuedSpansOnShutdownWithProcessResource(t *testing.T) {
	preserveGlobals(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	capture := &traceCapture{requests: make(chan *collectortrace.ExportTraceServiceRequest, 8)}
	server := grpc.NewServer()
	collectortrace.RegisterTraceServiceServer(server, capture)
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		if serveErr := <-serveDone; serveErr != nil && !errors.Is(serveErr, grpc.ErrServerStopped) {
			t.Errorf("OTLP test server: %v", serveErr)
		}
	})

	// Connection settings and enabled state come from Config, not exporter gates.
	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	t.Setenv("OTEL_SDK_DISABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "unused invalid endpoint")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "unused invalid endpoint")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "")
	t.Setenv("OTEL_SERVICE_NAME", "environment-service")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment.name=lab,k8s.pod.uid=test-pod,service.instance.id=not-a-process-id")
	t.Setenv("OTEL_TRACES_SAMPLER", "always_on")
	// No scheduled export and no full batch: only Shutdown can flush these spans.
	t.Setenv("OTEL_BSP_SCHEDULE_DELAY", "60000")
	t.Setenv("OTEL_BSP_MAX_QUEUE_SIZE", "32")
	t.Setenv("OTEL_BSP_MAX_EXPORT_BATCH_SIZE", "16")
	t.Setenv("OTEL_BSP_EXPORT_TIMEOUT", "3000")

	var previousInstance string
	for process := 0; process < 2; process++ {
		instance, initErr := Init(context.Background(), Config{
			Enabled:        true,
			ServiceName:    "sdk-test",
			ServiceVersion: "test-version",
			OTLPEndpoint:   "http://" + listener.Addr().String(),
			OTLPInsecure:   true,
		})
		if initErr != nil {
			t.Fatal(initErr)
		}
		if instance.TracerProvider == nil || instance.grpcConnection == nil {
			t.Fatal("enabled telemetry must own provider and connection")
		}
		ctx, parent := traces.Start(context.Background(), "parent")
		_, child := traces.Start(ctx, "child")
		traces.RecordError(child, errors.New("dependency failed"))
		child.End()
		parent.End()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		shutdownErr := instance.Shutdown(shutdownCtx)
		cancel()
		if shutdownErr != nil {
			t.Fatalf("flush during shutdown: %v", shutdownErr)
		}

		if instance.grpcConnection.GetState() != connectivity.Shutdown {
			t.Fatal("Shutdown must close its owned OTLP connection")
		}
		if err := instance.Shutdown(context.Background()); err != nil {
			t.Fatalf("repeated Shutdown: %v", err)
		}

		select {
		case request := <-capture.requests:
			if len(request.GetResourceSpans()) != 1 {
				t.Fatalf("resource batches = %d, want 1", len(request.GetResourceSpans()))
			}
			batch := request.GetResourceSpans()[0]
			attributes := stringAttributes(batch.GetResource().GetAttributes())
			for key, want := range map[string]string{
				"service.name":                "sdk-test",
				"service.namespace":           "hotelflow",
				"service.version":             "test-version",
				"deployment.environment.name": "lab",
				"k8s.pod.uid":                 "test-pod",
				"telemetry.sdk.language":      "go",
			} {
				if attributes[key] != want {
					t.Errorf("resource %s = %q, want %q", key, attributes[key], want)
				}
			}
			instanceID := attributes["service.instance.id"]
			if instanceID == "" || instanceID == "not-a-process-id" || instanceID == previousInstance {
				t.Fatalf("instance must be generated anew per process: %q", instanceID)
			}
			previousInstance = instanceID
			for _, attribute := range batch.GetResource().GetAttributes() {
				if attribute.GetKey() == "process.command_args" || attribute.GetKey() == "process.environment_variables" {
					t.Fatalf("resource must not include process secrets: %s", attribute.GetKey())
				}
			}
			if len(batch.GetScopeSpans()) != 1 || batch.GetScopeSpans()[0].GetScope().GetName() != "hotelflow/sdk-test" {
				t.Fatalf("tracer scope was not configured at startup: %v", batch.GetScopeSpans())
			}
			spans := batch.GetScopeSpans()[0].GetSpans()
			if len(spans) != 2 {
				t.Fatalf("flushed spans = %d, want 2", len(spans))
			}
			byName := make(map[string]*tracepb.Span, len(spans))
			for _, span := range spans {
				byName[span.GetName()] = span
			}
			gotParent, gotChild := byName["parent"], byName["child"]
			if gotParent == nil || gotChild == nil || string(gotChild.GetParentSpanId()) != string(gotParent.GetSpanId()) || string(gotChild.GetTraceId()) != string(gotParent.GetTraceId()) {
				t.Fatal("exported child lost its parent or trace")
			}
			if gotChild.GetStatus().GetCode() != tracepb.Status_STATUS_CODE_ERROR || len(gotChild.GetEvents()) != 1 {
				t.Fatal("RecordError must retain the failed operation in exported span")
			}
		case <-time.After(time.Second):
			t.Fatal("shutdown returned without exporting queued spans")
		}
	}
}

func stringAttributes(attributes []*commonpb.KeyValue) map[string]string {
	result := make(map[string]string, len(attributes))
	for _, attribute := range attributes {
		result[attribute.GetKey()] = attribute.GetValue().GetStringValue()
	}
	return result
}

func TestDisabledTelemetryReplacesRecordingProviderWithoutConnection(t *testing.T) {
	preserveGlobals(t)
	recorder := tracetest.NewSpanRecorder()
	previousProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(previousProvider)
	t.Cleanup(func() {
		_ = previousProvider.Shutdown(context.Background())
	})
	instance, err := Init(context.Background(), Config{
		ServiceName:  "offline",
		OTLPEndpoint: "unused invalid endpoint",
	})
	if err != nil {
		t.Fatalf("disabled telemetry must not require a Collector: %v", err)
	}
	if instance.TracerProvider == nil || instance.grpcConnection != nil {
		t.Fatal("disabled telemetry must expose a noop provider without opening a connection")
	}
	_, span := traces.Start(context.Background(), "offline-operation")
	if span.IsRecording() {
		t.Fatal("disabled telemetry retained the previous recording provider")
	}
	span.End()
	if len(recorder.Ended()) != 0 {
		t.Fatal("disabled telemetry exported through the previous provider")
	}
	if err := instance.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	var uninitialized *Telemetry
	if err := uninitialized.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestInitPreservesSDKSamplerEnvironment(t *testing.T) {
	preserveGlobals(t)
	t.Setenv("OTEL_TRACES_SAMPLER", "always_off")
	instance, err := Init(context.Background(), Config{
		Enabled:      true,
		ServiceName:  "unsampled",
		OTLPEndpoint: "127.0.0.1:1",
		OTLPInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, span := traces.Start(context.Background(), "unsampled-operation")
	if span.IsRecording() || span.SpanContext().IsSampled() {
		t.Fatal("Init must preserve the SDK sampler policy from environment")
	}
	span.End()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := instance.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

type propagationContextKey struct{}

func TestCarrierRoundTripPreservesWorkerContext(t *testing.T) {
	preserveGlobals(t)
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	producerCtx, producer := traces.Start(context.Background(), "producer")
	headers := map[string]string{"x-correlation-id": "business-event-id"}
	traces.Inject(producerCtx, headers)
	producer.End()

	workerCtx, cancel := context.WithCancel(context.WithValue(context.Background(), propagationContextKey{}, "transaction"))
	defer cancel()
	extracted := traces.Extract(workerCtx, headers)
	consumerCtx, consumer := traces.Start(extracted, "consumer")
	consumer.End()
	cancel()

	if headers["x-correlation-id"] != "business-event-id" || headers["traceparent"] == "" {
		t.Fatal("carrier must preserve business headers and carry trace context")
	}
	if consumerCtx.Value(propagationContextKey{}) != "transaction" || !errors.Is(consumerCtx.Err(), context.Canceled) {
		t.Fatal("extraction lost the worker context values or cancellation")
	}
	ended := recorder.Ended()
	if len(ended) != 2 || ended[1].Parent().SpanID() != ended[0].SpanContext().SpanID() || !ended[1].Parent().IsRemote() {
		t.Fatal("consumer must be the child of the remote producer")
	}
	if trace.SpanContextFromContext(consumerCtx).TraceID() != ended[0].SpanContext().TraceID() {
		t.Fatal("round trip changed trace ID")
	}
}
