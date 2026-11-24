package review_events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/IBM/sarama"
	"github.com/gofrs/uuid"

	event_router "hotels/internal/pkg/event-router"
	"hotels/internal/pkg/telemetry/traces"
)

type MessageHandler struct {
	router *event_router.EventRouter[string, []byte]
}

func NewMessageHandler(router *event_router.EventRouter[string, []byte]) *MessageHandler {
	return &MessageHandler{
		router: router,
	}
}

func (h *MessageHandler) Handle(ctx context.Context, _ sarama.ConsumerGroupSession, message *sarama.ConsumerMessage) (err error) {
	ctx, span := traces.Start(ctx, "handler.kafka.review_events")
	defer span.End()
	defer func() {
		traces.RecordError(span, err)
	}()

	var base BaseEvent
	if err = json.Unmarshal(message.Value, &base); err != nil {
		return err
	}

	// Как в Check-in, проверяем идентичность события в envelope и заголовке.
	var correlationID string
	for _, header := range message.Headers {
		if header != nil && string(header.Key) == "x-correlation-id" {
			correlationID = string(header.Value)
		}
	}
	if uuid.FromStringOrNil(correlationID) == uuid.Nil || correlationID != base.CorrelationID {
		return fmt.Errorf("некорректный correlation-id события Review")
	}
	if base.EventType == "" {
		return fmt.Errorf("не указан тип события Review")
	}

	// Без inbox: offset подтверждается consumer'ом только после успешного handler.
	return h.router.HandleEvent(ctx, base.EventType, message.Value)
}
