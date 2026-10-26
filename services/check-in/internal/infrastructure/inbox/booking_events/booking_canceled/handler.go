package booking_canceled

import (
	"context"

	"github.com/gofrs/uuid"

	"check-in/internal/application/usecase/checkin/confirm_cancellation"
	"check-in/internal/pkg/telemetry/traces"
)

const eventType = "booking.canceled.v1"

type Handler struct{ usecase *confirm_cancellation.UseCase }

func NewHandler(usecase *confirm_cancellation.UseCase) *Handler {
	return &Handler{usecase: usecase}
}

func (h *Handler) HandleEvent(ctx context.Context, body []byte) (err error) {
	ctx, span := traces.Start(ctx, "handler.event.booking_canceled")
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
		confirm_cancellation.Input{
			CheckInID:      uuid.FromStringOrNil(e.Payload.CheckInID),
			BookingID:      uuid.FromStringOrNil(e.BookingID),
			CheckInVersion: e.Payload.CheckInVersion,
		},
	)
}

func (h *Handler) EventType() string {
	return eventType
}
