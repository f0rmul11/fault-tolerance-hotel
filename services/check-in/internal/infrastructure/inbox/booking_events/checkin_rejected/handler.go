package checkin_rejected

import (
	"context"

	"github.com/gofrs/uuid"

	"check-in/internal/application/usecase/checkin/reject"
	"check-in/internal/pkg/telemetry/traces"
)

const eventType = "booking.checkin-rejected.v1"

type Handler struct{ usecase *reject.UseCase }

func NewHandler(usecase *reject.UseCase) *Handler {
	return &Handler{usecase: usecase}
}

func (h *Handler) HandleEvent(ctx context.Context, body []byte) (err error) {
	ctx, span := traces.Start(ctx, "handler.event.checkin_rejected")
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
		reject.Input{
			CheckInID:      uuid.FromStringOrNil(e.Payload.CheckInID),
			BookingID:      uuid.FromStringOrNil(e.BookingID),
			CheckInVersion: e.Payload.CheckInVersion,
			Message:        e.Payload.Message,
		},
	)
}

func (h *Handler) EventType() string {
	return eventType
}
