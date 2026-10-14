package booking_activated

import (
	"context"

	"github.com/gofrs/uuid"

	"check-in/internal/application/usecase/checkin/confirm_arrival"
)

const eventType = "booking.activated.v1"

type Handler struct{ usecase *confirm_arrival.UseCase }

func NewHandler(usecase *confirm_arrival.UseCase) *Handler {
	return &Handler{usecase: usecase}
}

func (h *Handler) HandleEvent(ctx context.Context, body []byte) error {
	e, err := deserialize(body)
	if err != nil {
		return err
	}
	return h.usecase.Execute(
		ctx,
		confirm_arrival.Input{
			CheckInID:      uuid.FromStringOrNil(e.CheckInID),
			BookingID:      uuid.FromStringOrNil(e.BookingID),
			CheckInVersion: e.CheckInVersion,
		},
	)
}

func (h *Handler) EventType() string {
	return eventType
}
