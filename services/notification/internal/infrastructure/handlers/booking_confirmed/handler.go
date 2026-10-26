package booking_confirmed

import (
	"context"

	"github.com/gofrs/uuid"

	"notification/internal/application/usecase/notification/confirmed"
	"notification/internal/pkg/telemetry/traces"
)

const eventType = "booking.confirmed.v1"

type Handler struct {
	usecase *confirmed.UseCase
}

func NewHandler(usecase *confirmed.UseCase) *Handler {
	return &Handler{
		usecase: usecase,
	}
}

func (h *Handler) HandleEvent(ctx context.Context, body []byte) (err error) {
	ctx, span := traces.Start(ctx, "handler.event.booking_confirmed")
	defer span.End()
	defer func() {
		traces.RecordError(span, err)
	}()

	event, err := deserialize(body)
	if err != nil {
		return err
	}
	return h.usecase.Execute(ctx, confirmed.Input{
		CorrelationID: uuid.FromStringOrNil(event.CorrelationID),
		BookingID:     uuid.FromStringOrNil(event.BookingID),
		RoomNumber:    event.Payload.RoomNumber,
		RoomType:      event.Payload.RoomType,
		StartsAt:      event.Payload.StartsAt,
		EndsAt:        event.Payload.EndsAt,
	})
}

func (h *Handler) EventType() string {
	return eventType
}
