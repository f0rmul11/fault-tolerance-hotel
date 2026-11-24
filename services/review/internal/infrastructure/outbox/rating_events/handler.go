package rating_events

import (
	"context"

	"review/config"
	"review/internal/pkg/event"
)

type Producer interface {
	Handle(context.Context, event.Events) error
}

type Handler struct {
	producer  Producer
	batchSize int
}

func NewHandler(producer Producer, batchSize int) *Handler {
	return &Handler{
		producer:  producer,
		batchSize: batchSize,
	}
}

func (h *Handler) Topic() string {
	return config.RatingEventsTopic
}

func (h *Handler) BatchSize(context.Context) int {
	return h.batchSize
}

func (h *Handler) Handle(ctx context.Context, events event.Events) error {
	return h.producer.Handle(ctx, events)
}
