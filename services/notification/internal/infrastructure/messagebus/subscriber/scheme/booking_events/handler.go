package booking_events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/IBM/sarama"
	"github.com/gofrs/uuid"

	event_router "notification/internal/pkg/event-router"
)

type MessageHandler struct {
	router *event_router.EventRouter[string, []byte]
}

func NewMessageHandler(router *event_router.EventRouter[string, []byte]) *MessageHandler {
	return &MessageHandler{
		router: router,
	}
}

func (h *MessageHandler) Handle(ctx context.Context, _ sarama.ConsumerGroupSession, message *sarama.ConsumerMessage) error {
	var base BaseEvent
	if err := json.Unmarshal(message.Value, &base); err != nil {
		return err
	}

	var correlationID string
	for _, header := range message.Headers {
		if string(header.Key) == correlationIDHeader {
			correlationID = string(header.Value)
			break
		}
	}
	if uuid.FromStringOrNil(correlationID) == uuid.Nil {
		return fmt.Errorf("в событии отсутствует корректный x-correlation-id")
	}
	if correlationID != base.CorrelationID {
		return fmt.Errorf("correlation-id в заголовке и событии не совпадают")
	}
	if uuid.FromStringOrNil(base.EntityID) == uuid.Nil {
		return fmt.Errorf("в событии отсутствует идентификатор брони")
	}

	return h.router.HandleEvent(ctx, base.EventType, message.Value)
}
