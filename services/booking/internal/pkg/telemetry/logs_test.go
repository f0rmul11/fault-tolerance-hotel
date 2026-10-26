package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go.opentelemetry.io/otel"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"booking/internal/pkg/telemetry/traces"
	collectorlog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	logpb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

type logCapture struct {
	collectorlog.UnimplementedLogsServiceServer
	requests chan *collectorlog.ExportLogsServiceRequest
}

func (c *logCapture) Export(_ context.Context, request *collectorlog.ExportLogsServiceRequest) (*collectorlog.ExportLogsServiceResponse, error) {
	c.requests <- request
	return &collectorlog.ExportLogsServiceResponse{}, nil
}

func TestLogsFanoutCorrelationFilteringAndShutdown(t *testing.T) {
	preserveGlobals(t)
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = stdout
	t.Cleanup(func() {
		os.Stdout = previous
		_ = stdout.Close()
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	logs := &logCapture{requests: make(chan *collectorlog.ExportLogsServiceRequest, 8)}
	spans := &traceCapture{requests: make(chan *collectortrace.ExportTraceServiceRequest, 8)}
	server := grpc.NewServer()
	collectorlog.RegisterLogsServiceServer(server, logs)
	collectortrace.RegisterTraceServiceServer(server, spans)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(server.Stop)
	// Neither queue can fill or reach its scheduled flush before Shutdown.
	t.Setenv("OTEL_BSP_SCHEDULE_DELAY", "60000")
	t.Setenv("OTEL_BLRP_SCHEDULE_DELAY", "60000")
	t.Setenv("OTEL_BLRP_MAX_EXPORT_BATCH_SIZE", "512")
	instance, err := Init(context.Background(), Config{
		Enabled:      true,
		LogsEnabled:  true,
		LogLevel:     "warn",
		ServiceName:  "logs-test",
		OTLPEndpoint: listener.Addr().String(),
		OTLPInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, span := traces.Start(context.Background(), "request")
	sc := span.SpanContext()
	slog.InfoContext(ctx, "filtered")
	slog.WarnContext(ctx, "correlated", "request_id", "request-1", "attempt", 2)
	slog.With("component", "test").WithGroup("details").ErrorContext(ctx, "grouped", "reason", "unavailable")
	slog.Warn("startup")
	otel.Handle(errors.New("exporter diagnostic must stay out of OTLP"))
	span.End()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := instance.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	if instance.grpcConnection.GetState() != connectivity.Shutdown {
		t.Fatal("shared connection is still open")
	}
	var request *collectorlog.ExportLogsServiceRequest
	select {
	case request = <-logs.requests:
	default:
		t.Fatal("Shutdown did not export pending logs")
	}
	if len(request.ResourceLogs) != 1 {
		t.Fatalf("resource logs = %d", len(request.ResourceLogs))
	}
	resource := request.ResourceLogs[0]
	if stringAttributes(resource.Resource.Attributes)["service.name"] != "logs-test" {
		t.Fatal("logs resource identity differs from configuration")
	}
	records := make(map[string]*logpb.LogRecord)
	for _, scope := range resource.ScopeLogs {
		for _, record := range scope.LogRecords {
			records[record.Body.GetStringValue()] = record
		}
	}
	if len(records) != 3 || records["filtered"] != nil {
		t.Fatalf("level filtering or duplication: %v", records)
	}
	for _, name := range []string{"correlated", "grouped"} {
		record := records[name]
		tid, sid := sc.TraceID(), sc.SpanID()
		if !bytes.Equal(record.TraceId, tid[:]) || !bytes.Equal(record.SpanId, sid[:]) {
			t.Fatalf("%s: native trace/span IDs lost", name)
		}
	}
	if len(records["startup"].TraceId) != 0 || len(records["startup"].SpanId) != 0 {
		t.Fatal("startup log acquired a fabricated trace")
	}
	if records["correlated"].SeverityNumber != logpb.SeverityNumber_SEVERITY_NUMBER_WARN || stringAttributes(records["correlated"].Attributes)["request_id"] != "request-1" {
		t.Fatal("severity or attributes were lost")
	}
	select {
	case <-spans.requests:
	default:
		t.Fatal("trace flush was lost when adding logs")
	}
	if _, err := stdout.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(stdout)
	count := 0
	for decoder.More() {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		count++
		if record["msg"] == "correlated" && (record["trace_id"] != sc.TraceID().String() || record["span_id"] != sc.SpanID().String()) {
			t.Fatal("stdout correlation lost")
		}
	}
	if count != 3 {
		t.Fatalf("stdout count = %d, want 3", count)
	}
}

func TestLogsCanBeDisabledWithoutDisablingTraces(t *testing.T) {
	preserveGlobals(t)
	instance, err := Init(context.Background(), Config{
		Enabled:      true,
		LogsEnabled:  false,
		ServiceName:  "traces-only",
		OTLPEndpoint: "127.0.0.1:1",
		OTLPInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if instance.LoggerProvider != nil || instance.TracerProvider == nil {
		t.Fatal("signal enablement is not independent")
	}
	if err := instance.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
