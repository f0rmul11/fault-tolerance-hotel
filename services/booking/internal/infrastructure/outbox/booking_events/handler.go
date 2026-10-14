package booking_events

import (
	"context"

	"booking/internal/pkg/event"
	"booking/internal/pkg/msgbus/producer"
)

type Handler struct {
	topic     string
	batchSize int

	producer *producer.MessageProducer
}

func NewHandler(topic string, batchSize int, producer *producer.MessageProducer) *Handler {
	return &Handler{
		topic:     topic,
		batchSize: batchSize,
		producer:  producer,
	}
}

func (h *Handler) Handle(ctx context.Context, events event.Events) error {
	return h.producer.Handle(ctx, events)
}

func (h *Handler) Topic() string {
	return h.topic
}

func (h *Handler) BatchSize(_ context.Context) int {
	return h.batchSize
}
