package checkin_events

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"booking/internal/pkg/telemetry/traces"
)

func TestInboxPersistsHandlerContextWithoutMutatingReceivedHeaders(t *testing.T) {
	previous := otel.GetTracerProvider()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	ctx, receive := provider.Tracer("test").Start(t.Context(), "kafka.receive")
	defer receive.End()
	headers := map[string]string{
		"x-correlation-id": "de3dab97-8166-4908-97ab-ce3e20f7df75",
		"x-custom":         "kept",
	}
	traces.Inject(ctx, headers)
	incoming := &sarama.ConsumerMessage{
		Topic: "events.v1",
		Value: []byte(`{"booking_id":"entity","correlation_id":"de3dab97-8166-4908-97ab-ce3e20f7df75"}`),
	}
	for key, value := range headers {
		incoming.Headers = append(incoming.Headers, &sarama.RecordHeader{
			Key:   []byte(key),
			Value: []byte(value),
		})
	}
	inbox := &inboxFake{}
	if err := NewMessageHandler(inbox).Handle(ctx, nil, incoming); err != nil {
		t.Fatal(err)
	}
	var persisted map[string]string
	if err := json.Unmarshal(inbox.messages[0].GetHeaders(), &persisted); err != nil {
		t.Fatal(err)
	}
	saved := trace.SpanContextFromContext(traces.Extract(t.Context(), persisted))
	if saved.TraceID() != receive.SpanContext().TraceID() || saved.SpanID() == receive.SpanContext().SpanID() {
		t.Fatal("inbox did not persist the active handler span")
	}
	found := false
	for _, span := range exporter.GetSpans() {
		if span.SpanContext.SpanID() == saved.SpanID() {
			found = span.Parent.SpanID() == receive.SpanContext().SpanID()
		}
	}
	if !found || persisted["x-custom"] != "kept" || persisted["x-correlation-id"] != headers["x-correlation-id"] {
		t.Fatal("parent chain or event headers lost")
	}
	for _, header := range incoming.Headers {
		if string(header.Value) != headers[string(header.Key)] {
			t.Fatal("original Kafka headers mutated")
		}
	}
}
