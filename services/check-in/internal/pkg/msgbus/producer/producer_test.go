package producer

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"check-in/internal/pkg/event"
	"check-in/internal/pkg/telemetry/traces"
)

type producerFake struct {
	sarama.SyncProducer
	messages [][]*sarama.ProducerMessage
	failure  error
	partial  bool
}

func (p *producerFake) SendMessages(messages []*sarama.ProducerMessage) error {
	p.messages = append(p.messages, messages)
	if p.partial {
		return sarama.ProducerErrors{&sarama.ProducerError{
			Msg: messages[1],
			Err: p.failure,
		}}
	}
	return p.failure
}

func traceProvider(t *testing.T) (*sdktrace.TracerProvider, *tracetest.InMemoryExporter) {
	t.Helper()
	previous := otel.GetTracerProvider()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	return provider, exporter
}

func TestBatchRetainsIndependentParentsAndRetryIdentity(t *testing.T) {
	provider, exporter := traceProvider(t)
	events := make(event.Events, 0, 2)
	parents := make([]trace.SpanContext, 0, 2)
	for _, identity := range []string{"first-event", "second-event"} {
		ctx, parent := provider.Tracer("test").Start(t.Context(), "request")
		headers := map[string]string{
			"x-correlation-id": identity,
			"x-custom":         "preserved",
		}
		traces.Inject(ctx, headers)
		raw, err := json.Marshal(headers)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event.Event{
			Schema:  "events.v1",
			Headers: raw,
			Key:     event.Raw(identity),
			Body:    event.Raw("body"),
		})
		parents = append(parents, parent.SpanContext())
		parent.End()
	}
	original := []string{string(events[0].Headers), string(events[1].Headers)}
	fake := &producerFake{}
	producer := &MessageProducer{producer: fake}
	for attempt := 0; attempt < 2; attempt++ {
		if err := producer.Handle(t.Context(), events); err != nil {
			t.Fatal(err)
		}
	}
	if len(fake.messages) != 2 || len(fake.messages[0]) != 2 {
		t.Fatal("batch sending was changed")
	}
	var attempts []trace.SpanContext
	for attempt, messages := range fake.messages {
		for index, message := range messages {
			headers := make(map[string]string)
			for _, header := range message.Headers {
				headers[string(header.Key)] = string(header.Value)
			}
			outgoing := trace.SpanContextFromContext(traces.Extract(t.Context(), headers))
			if outgoing.TraceID() != parents[index].TraceID() || outgoing.SpanID() == parents[index].SpanID() {
				t.Fatalf("attempt %d message %d lost its own producer context", attempt, index)
			}
			key, _ := message.Key.Encode()
			if headers["x-correlation-id"] != string(key) || headers["x-custom"] != "preserved" {
				t.Fatal("event identity or custom headers changed")
			}
			attempts = append(attempts, outgoing)
		}
	}
	if attempts[0].SpanID() == attempts[2].SpanID() {
		t.Fatal("retry reused the previous attempt span")
	}
	for index := range events {
		if string(events[index].Headers) != original[index] {
			t.Fatal("saved outbox headers were mutated")
		}
	}
	published := 0
	for _, span := range exporter.GetSpans() {
		if span.Name != "kafka.publish" {
			continue
		}
		published++
		parent := parents[0]
		if span.SpanContext.TraceID() == parents[1].TraceID() {
			parent = parents[1]
		}
		if span.Parent.SpanID() != parent.SpanID() || span.SpanKind != trace.SpanKindProducer {
			t.Fatal("producer span has the wrong parent or kind")
		}
	}
	if published != 4 {
		t.Fatalf("got %d producer spans", published)
	}
}

func TestProducerFailureMarksOnlyAffectedSpans(t *testing.T) {
	failure := errors.New("send failed")
	for _, test := range []struct {
		name    string
		partial bool
		failed  int
	}{
		{name: "partial batch", partial: true, failed: 1},
		{name: "whole batch", failed: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, exporter := traceProvider(t)
			producer := &MessageProducer{producer: &producerFake{
				failure: failure,
				partial: test.partial,
			}}
			events := event.Events{
				{Schema: "events.v1", Headers: event.Raw("{}")},
				{Schema: "events.v1", Headers: event.Raw("{}")},
			}
			if producer.Handle(t.Context(), events) == nil {
				t.Fatal("send error was lost")
			}
			failed := 0
			for _, span := range exporter.GetSpans() {
				if span.Status.Code == codes.Error {
					failed++
				}
			}
			if failed != test.failed {
				t.Fatalf("marked %d failed spans, want %d", failed, test.failed)
			}
		})
	}
}
