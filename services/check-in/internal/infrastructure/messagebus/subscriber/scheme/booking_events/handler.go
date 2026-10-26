package booking_events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/IBM/sarama"
	"github.com/gofrs/uuid"

	"check-in/internal/pkg/inbox/message"
	"check-in/internal/pkg/telemetry/traces"
)

type messageSaver interface {
	SaveMessages(context.Context, message.Messages) error
}

type MessageHandler struct {
	inbox messageSaver
}

func NewMessageHandler(inbox messageSaver) *MessageHandler {
	return &MessageHandler{inbox: inbox}
}

func (h *MessageHandler) Handle(ctx context.Context, _ sarama.ConsumerGroupSession, kafkaMessage *sarama.ConsumerMessage) (err error) {
	ctx, span := traces.Start(ctx, "handler.kafka.booking_events")
	defer span.End()
	defer func() {
		traces.RecordError(span, err)
	}()

	var base BaseEvent
	if err := json.Unmarshal(kafkaMessage.Value, &base); err != nil {
		return err
	}
	headers := make(map[string]string, len(kafkaMessage.Headers))
	for _, header := range kafkaMessage.Headers {
		headers[string(header.Key)] = string(header.Value)
	}
	correlationID := headers[correlationIDHeader]
	if uuid.FromStringOrNil(correlationID) == uuid.Nil {
		return fmt.Errorf("в событии отсутствует корректный x-correlation-id")
	}
	if correlationID != base.CorrelationID {
		return fmt.Errorf("correlation-id в заголовке и событии не совпадают")
	}
	// Resume the inbox worker below this handler without mutating Kafka headers.
	traces.Inject(ctx, headers)
	headersRaw, err := json.Marshal(headers)
	if err != nil {
		return err
	}
	msg := message.NewMessage(
		base.BookingID,
		correlationID,
		kafkaMessage.Topic,
		kafkaMessage.Key,
		kafkaMessage.Value,
		kafkaMessage.Timestamp,
		message.WithHeaders(headersRaw),
	)
	return h.inbox.SaveMessages(ctx, message.Messages{msg})
}
