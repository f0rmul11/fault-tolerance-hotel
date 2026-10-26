package checkin_events

import (
	"context"
	"encoding/json"

	"go.opentelemetry.io/otel/trace"

	event_router "booking/internal/pkg/event-router"
	"booking/internal/pkg/inbox/message"
	"booking/internal/pkg/telemetry/traces"
)

type BaseEvent struct {
	EventType string          `json:"event_type"`
	BookingID string          `json:"booking_id"`
	Payload   json.RawMessage `json:"payload"`
}

type Handler struct {
	topic     string
	batchSize int

	router *event_router.EventRouter[string, []byte]
}

func NewHandler(topic string, batchSize int, router *event_router.EventRouter[string, []byte]) *Handler {
	return &Handler{
		topic:     topic,
		batchSize: batchSize,
		router:    router,
	}
}

func (h *Handler) Handle(ctx context.Context, messages message.Messages) error {
	for _, msg := range messages {
		if err := h.handleMessage(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) handleMessage(ctx context.Context, msg *message.Message) (err error) {
	headers := make(map[string]string)
	_ = json.Unmarshal(msg.GetHeaders(), &headers)
	// Extract over the live worker context: cancellation and transaction values remain.
	ctx = traces.Extract(ctx, headers)
	ctx, span := traces.Start(ctx, "handler.inbox.checkin_events",
		trace.WithSpanKind(trace.SpanKindConsumer),
	)
	defer span.End()
	defer func() {
		traces.RecordError(span, err)
	}()

	var base BaseEvent
	if err := json.Unmarshal(msg.GetBody(), &base); err != nil {
		return err
	}
	return h.router.HandleEvent(ctx, base.EventType, msg.GetBody())
}

func (h *Handler) Topic() string {
	return h.topic
}

func (h *Handler) BatchSize(_ context.Context) int {
	return h.batchSize
}
