package booking_events

import (
	"context"
	"errors"

	"github.com/IBM/sarama"

	"notification/internal/dto/bookingevent"
	event_router "notification/internal/pkg/event-router"
	"notification/internal/pkg/msgbus/subscriber/retry"
	"notification/internal/pkg/notificationerror"
	"notification/internal/pkg/telemetry/traces"
)

type MessageHandler struct {
	router *event_router.EventRouter[string, bookingevent.Event]
}

func NewMessageHandler(router *event_router.EventRouter[string, bookingevent.Event]) *MessageHandler {
	return &MessageHandler{
		router: router,
	}
}

func (h *MessageHandler) Handle(ctx context.Context, message *sarama.ConsumerMessage, headers map[string]string) (err error) {
	ctx, span := traces.Start(ctx, "handler.kafka.booking_events")
	defer span.End()
	defer func() {
		traces.RecordError(span, err)
	}()

	event, err := decodeEvent(message.Value, headers)
	if err != nil {
		return retry.DLQErr("invalid_event", err)
	}

	err = h.router.HandleEvent(ctx, event.Type, event)
	switch {
	case notificationerror.IsCode(err, notificationerror.InvalidArgument):
		return retry.DLQErr("invalid_event", err)
	case errors.Is(err, context.DeadlineExceeded):
		return retry.RetryErr("provider_timeout", context.DeadlineExceeded)
	default:
		return err
	}
}
