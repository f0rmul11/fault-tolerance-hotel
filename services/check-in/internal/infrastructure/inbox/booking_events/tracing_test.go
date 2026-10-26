package booking_events

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	event_router "check-in/internal/pkg/event-router"
	"check-in/internal/pkg/inbox/message"
	"check-in/internal/pkg/telemetry/traces"
)

type workerContextKey struct{}

type observingHandler struct {
	contexts []context.Context
}

func (h *observingHandler) EventType() string {
	return "test.event"
}

func (h *observingHandler) HandleEvent(ctx context.Context, _ []byte) error {
	h.contexts = append(h.contexts, ctx)
	return nil
}

func TestInboxBatchRestoresEachParentAndPreservesWorkerContext(t *testing.T) {
	previous := otel.GetTracerProvider()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	batch := make(message.Messages, 0, 2)
	parents := make([]trace.SpanContext, 0, 2)
	for range 2 {
		ctx, parent := provider.Tracer("test").Start(t.Context(), "subscriber")
		headers := make(map[string]string)
		traces.Inject(ctx, headers)
		raw, err := json.Marshal(headers)
		if err != nil {
			t.Fatal(err)
		}
		batch = append(batch, message.NewMessage("entity", "event", "events.v1", nil,
			[]byte(`{"event_type":"test.event"}`), time.Now(), message.WithHeaders(raw),
		))
		parents = append(parents, parent.SpanContext())
		parent.End()
	}
	observer := &observingHandler{}
	router := event_router.NewEventRouter[string, []byte]()
	router.RegisterAll(observer)
	handler := NewHandler("events.v1", 10, router)
	workerCtx, cancel := context.WithCancel(context.WithValue(t.Context(), workerContextKey{}, "worker"))
	defer cancel()
	if err := handler.Handle(workerCtx, batch); err != nil {
		t.Fatal(err)
	}
	if len(observer.contexts) != 2 {
		t.Fatalf("received %d messages", len(observer.contexts))
	}
	cancel()
	for index, ctx := range observer.contexts {
		if trace.SpanContextFromContext(ctx).TraceID() != parents[index].TraceID() {
			t.Fatal("messages in the same batch were joined into one trace")
		}
		if ctx.Value(workerContextKey{}) != "worker" || ctx.Err() != context.Canceled {
			t.Fatal("extraction lost worker values or cancellation")
		}
	}
	processed := 0
	for _, span := range exporter.GetSpans() {
		if span.Name != "handler.inbox.booking_events" {
			continue
		}
		if span.Parent.SpanID() != parents[processed].SpanID() {
			t.Fatal("inbox processing lost the persisted subscriber parent")
		}
		processed++
	}
	if processed != 2 {
		t.Fatalf("got %d inbox spans", processed)
	}
}
