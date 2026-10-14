package booking_canceled

import (
	"context"

	"github.com/gofrs/uuid"

	"notification/internal/application/usecase/notification/canceled"
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

func (h *Handler) HandleEvent(ctx context.Context, body []byte) error {
	event, err := deserialize(body)
	if err != nil {
		return err
	}
	return h.usecase.Execute(ctx, canceled.Input{
		CorrelationID: uuid.FromStringOrNil(event.CorrelationID),
		BookingID:     uuid.FromStringOrNil(event.BookingID),
	})
}

func (h *Handler) EventType() string {
	return eventType
}
