package booking_confirmed

import (
	"context"
	"strconv"

	"github.com/gofrs/uuid"

	"check-in/internal/application/usecase/checkin/register"
	"check-in/internal/domain/vo"
)

const eventType = "booking.confirmed.v1"

type Handler struct{ usecase *register.UseCase }

func NewHandler(usecase *register.UseCase) *Handler {
	return &Handler{usecase: usecase}
}

func (h *Handler) HandleEvent(ctx context.Context, body []byte) error {
	e, err := deserialize(body)
	if err != nil {
		return err
	}
	roomID, err := strconv.ParseInt(e.Payload.RoomID, 10, 64)
	if err != nil {
		return err
	}

	period, err := vo.ParsePeriod(e.Payload.StartsAt, e.Payload.EndsAt)
	if err != nil {
		return err
	}
	return h.usecase.Execute(
		ctx,
		register.Input{
			BookingID:  uuid.FromStringOrNil(e.BookingID),
			RoomID:     roomID,
			RoomNumber: e.Payload.RoomNumber,
			RoomType:   e.Payload.RoomType,
			GuestID:    uuid.FromStringOrNil(e.Payload.GuestID),
			Period:     period,
		},
	)
}

func (h *Handler) EventType() string {
	return eventType
}
