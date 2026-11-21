package booking_events

import (
	"context"

	"booking/internal/pkg/event"
	"booking/internal/pkg/msgbus/producer"
)

// Handler связывает outbox топика событий бронирования с Kafka-продьюсером.
type Handler struct {
	topic           string
	batchSize       int
	partitionsCount int32

	producer *producer.MessageProducer
}

func NewHandler(
	topic string,
	batchSize int,
	partitionsCount int32,
	producer *producer.MessageProducer,
) *Handler {
	return &Handler{
		topic:           topic,
		batchSize:       batchSize,
		partitionsCount: partitionsCount,
		producer:        producer,
	}
}

func (h *Handler) Topic() string {
	return h.topic
}

func (h *Handler) BatchSize(context.Context) int {
	return h.batchSize
}

func (h *Handler) PartitionsCount() int32 {
	return h.partitionsCount
}

// Partition возвращает false, чтобы outbox выбрал партицию по ключу booking_id.
func (h *Handler) Partition() (int32, bool) {
	return 0, false
}

// HandlePartition отправляет события в партицию, сохранённую при записи в outbox.
// Возвращённая ошибка оставляет курсор на месте до следующего тика.
func (h *Handler) HandlePartition(ctx context.Context, partitionID int32, events event.Events) error {
	return h.producer.HandlePartition(ctx, partitionID, events)
}
