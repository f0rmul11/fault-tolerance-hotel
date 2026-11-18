package booking_canceled

import (
	"context"

	"notification/internal/application/usecase/notification/canceled"
	"notification/internal/dto/bookingevent"
	"notification/internal/pkg/telemetry/traces"
)

const eventType = "booking.canceled.v1"

type Handler struct {
	usecase *canceled.UseCase
}

func NewHandler(usecase *canceled.UseCase) *Handler {
	return &Handler{
		usecase: usecase,
	}
}

func (h *Handler) HandleEvent(ctx context.Context, event bookingevent.Event) (err error) {
	ctx, span := traces.Start(ctx, "handler.event.booking_canceled")
	defer span.End()
	defer func() {
		traces.RecordError(span, err)
	}()

	return h.usecase.Execute(ctx, canceled.Input{
		CorrelationID: event.CorrelationID,
		BookingID:     event.BookingID,
	})
}

func (h *Handler) EventType() string {
	return eventType
}
