package checkin_events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/IBM/sarama"
	"github.com/gofrs/uuid"

	"booking/internal/pkg/inbox"
	"booking/internal/pkg/inbox/message"
)

type MessageHandler struct {
	inbox *inbox.Inbox
}

func NewMessageHandler(inbox *inbox.Inbox) *MessageHandler {
	return &MessageHandler{inbox: inbox}
}

func (h *MessageHandler) Handle(ctx context.Context, _ sarama.ConsumerGroupSession, kafkaMessage *sarama.ConsumerMessage) error {
	var base BaseEvent
	if err := json.Unmarshal(kafkaMessage.Value, &base); err != nil {
		return err
	}
	var correlationID string
	for _, header := range kafkaMessage.Headers {
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
	msg := message.NewMessage(
		base.EntityID,
		correlationID,
		kafkaMessage.Topic,
		kafkaMessage.Key,
		kafkaMessage.Value,
		kafkaMessage.Timestamp,
	)
	return h.inbox.SaveMessages(ctx, message.Messages{msg})
}
