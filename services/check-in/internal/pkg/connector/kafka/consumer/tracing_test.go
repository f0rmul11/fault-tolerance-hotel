package consumer

import (
	"context"
	"testing"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type sessionContextKey struct{}

type traceSessionFake struct {
	sarama.ConsumerGroupSession
	ctx       context.Context
	marked    int
	committed int
}

func (s *traceSessionFake) Context() context.Context {
	return s.ctx
}

func (s *traceSessionFake) MarkMessage(_ *sarama.ConsumerMessage, _ string) {
	s.marked++
}

func (s *traceSessionFake) Commit() {
	s.committed++
}

type traceClaimFake struct {
	sarama.ConsumerGroupClaim
	messages chan *sarama.ConsumerMessage
}

func (c traceClaimFake) Messages() <-chan *sarama.ConsumerMessage {
	return c.messages
}

func TestConsumerExtractsEachMessageAndKeepsSessionCancellation(t *testing.T) {
	previous := otel.GetTracerProvider()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	ids := []string{"4bf92f3577b34da6a3ce929d0e0e4736", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	messages := make(chan *sarama.ConsumerMessage, len(ids))
	for _, id := range ids {
		messages <- &sarama.ConsumerMessage{
			Topic: "events.v1",
			Headers: []*sarama.RecordHeader{&sarama.RecordHeader{
				Key:   []byte("traceparent"),
				Value: []byte("00-" + id + "-00f067aa0ba902b7-01"),
			}},
		}
	}
	close(messages)
	sessionCtx, cancel := context.WithCancel(context.WithValue(t.Context(), sessionContextKey{}, "session"))
	defer cancel()
	session := &traceSessionFake{ctx: sessionCtx}
	var contexts []context.Context
	group := groupSubscriber{messageHandler: func(ctx context.Context, _ sarama.ConsumerGroupSession, _ *sarama.ConsumerMessage) error {
		contexts = append(contexts, ctx)
		return nil
	}}
	if err := group.ConsumeClaim(session, traceClaimFake{messages: messages}); err != nil {
		t.Fatal(err)
	}
	if len(contexts) != len(ids) || session.marked != 2 || session.committed != 2 {
		t.Fatal("message dispatch or acknowledgement changed")
	}
	cancel()
	for index, ctx := range contexts {
		if trace.SpanContextFromContext(ctx).TraceID().String() != ids[index] {
			t.Fatal("consumer mixed parents of separate messages")
		}
		if ctx.Value(sessionContextKey{}) != "session" || ctx.Err() != context.Canceled {
			t.Fatal("session cancellation or values lost")
		}
	}
	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("got %d spans", len(spans))
	}
	for _, span := range spans {
		if span.Parent.SpanID().String() != "00f067aa0ba902b7" || span.SpanKind != trace.SpanKindConsumer {
			t.Fatal("receive span parent or kind is wrong")
		}
	}
}
