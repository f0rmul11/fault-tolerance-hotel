package booking_activated

import (
	"context"

	"notification/internal/application/usecase/notification/activated"
	"notification/internal/dto/bookingevent"
	"notification/internal/pkg/telemetry/traces"
)

const eventType = "booking.activated.v1"

type Handler struct {
	usecase *activated.UseCase
}

func NewHandler(usecase *activated.UseCase) *Handler {
	return &Handler{
		usecase: usecase,
	}
}

func (h *Handler) HandleEvent(ctx context.Context, event bookingevent.Event) (err error) {
	ctx, span := traces.Start(ctx, "handler.event.booking_activated")
	defer span.End()
	defer func() {
		traces.RecordError(span, err)
	}()

	return h.usecase.Execute(ctx, activated.Input{
		CorrelationID: event.CorrelationID,
		BookingID:     event.BookingID,
	})
}

func (h *Handler) EventType() string {
	return eventType
}
