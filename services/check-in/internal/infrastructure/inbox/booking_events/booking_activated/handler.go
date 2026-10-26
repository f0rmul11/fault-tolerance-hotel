package booking_activated

import (
	"context"

	"github.com/gofrs/uuid"

	"check-in/internal/application/usecase/checkin/confirm_arrival"
	"check-in/internal/pkg/telemetry/traces"
)

const eventType = "booking.activated.v1"

type Handler struct{ usecase *confirm_arrival.UseCase }

func NewHandler(usecase *confirm_arrival.UseCase) *Handler {
	return &Handler{usecase: usecase}
}

func (h *Handler) HandleEvent(ctx context.Context, body []byte) (err error) {
	ctx, span := traces.Start(ctx, "handler.event.booking_activated")
	defer span.End()
	defer func() {
		traces.RecordError(span, err)
	}()

	e, err := deserialize(body)
	if err != nil {
		return err
	}
	return h.usecase.Execute(
		ctx,
		confirm_arrival.Input{
			CheckInID:      uuid.FromStringOrNil(e.Payload.CheckInID),
			BookingID:      uuid.FromStringOrNil(e.BookingID),
			CheckInVersion: e.Payload.CheckInVersion,
		},
	)
}

func (h *Handler) EventType() string {
	return eventType
}
