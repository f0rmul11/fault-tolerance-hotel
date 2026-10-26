package arrival_requested

import (
	"context"

	"github.com/gofrs/uuid"

	"booking/internal/application/usecase/booking/activate"
	checkindto "booking/internal/dto/checkin"
	"booking/internal/pkg/telemetry/traces"
)

const eventType = "checkin.arrival-requested.v1"

type Handler struct{ usecase *activate.UseCase }

func NewHandler(usecase *activate.UseCase) *Handler {
	return &Handler{usecase: usecase}
}

func (h *Handler) HandleEvent(ctx context.Context, body []byte) (err error) {
	ctx, span := traces.Start(ctx, "handler.event.arrival_requested")
	defer span.End()
	defer func() {
		traces.RecordError(span, err)
	}()

	envelope, err := deserialize(body)
	if err != nil {
		return err
	}
	e := envelope.Payload
	return h.usecase.Execute(ctx, activate.Input{Request: checkindto.Request{
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
