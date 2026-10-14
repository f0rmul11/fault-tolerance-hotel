package checkin_rejected

import (
	"context"

	"github.com/gofrs/uuid"

	"check-in/internal/application/usecase/checkin/reject"
)

const eventType = "booking.checkin-rejected.v1"

type Handler struct{ usecase *reject.UseCase }

func NewHandler(usecase *reject.UseCase) *Handler {
	return &Handler{usecase: usecase}
}

func (h *Handler) HandleEvent(ctx context.Context, body []byte) error {
	e, err := deserialize(body)
	if err != nil {
		return err
	}
	return h.usecase.Execute(
		ctx,
		reject.Input{
			CheckInID:      uuid.FromStringOrNil(e.CheckInID),
			BookingID:      uuid.FromStringOrNil(e.BookingID),
			CheckInVersion: e.CheckInVersion,
			Message:        e.Message,
		},
	)
}

func (h *Handler) EventType() string {
	return eventType
}
