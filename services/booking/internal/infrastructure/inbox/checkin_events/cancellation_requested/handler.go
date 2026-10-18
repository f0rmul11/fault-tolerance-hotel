package cancellation_requested

import (
	"context"

	"github.com/gofrs/uuid"

	"booking/internal/application/usecase/booking/cancel"
	checkindto "booking/internal/dto/checkin"
)

const eventType = "checkin.cancellation-requested.v1"

type Handler struct{ usecase *cancel.UseCase }

func NewHandler(usecase *cancel.UseCase) *Handler {
	return &Handler{usecase: usecase}
}

func (h *Handler) HandleEvent(ctx context.Context, body []byte) error {
	envelope, err := deserialize(body)
	if err != nil {
		return err
	}
	e := envelope.Payload
	return h.usecase.Execute(ctx, cancel.Input{Request: checkindto.Request{
		CorrelationID:  uuid.FromStringOrNil(envelope.CorrelationID),
		CheckInID:      uuid.FromStringOrNil(e.CheckInID),
		BookingID:      uuid.FromStringOrNil(envelope.BookingID),
		CheckInVersion: e.CheckInVersion,
		ActorID:        uuid.FromStringOrNil(e.ActorID),
		RequestID:      uuid.FromStringOrNil(envelope.RequestID),
		RequestedAt:    e.RequestedAt,
	}})
}

func (h *Handler) EventType() string {
	return eventType
}
