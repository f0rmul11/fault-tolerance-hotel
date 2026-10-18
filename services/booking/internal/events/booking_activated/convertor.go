package booking_activated

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gofrs/uuid"

	"booking/config"
	"booking/internal/domain/entity"
	checkindto "booking/internal/dto/checkin"
	"booking/internal/events"
	"booking/internal/pkg/event"
	"booking/internal/pkg/pipe"
)

const Type = "booking.activated.v1"

func New(b *entity.Booking, request checkindto.Request) pipe.Func[event.Events] {
	return func(ctx context.Context, batch event.Events) (event.Events, error) {
		correlationID, err := uuid.NewV4()
		if err != nil {
			return nil, err
		}
		baseEvent := events.Base[BookingActivatedEvent]{
			CorrelationID:  correlationID.String(),
			EventType:      Type,
			BookingID:      b.ID().String(),
			BookingVersion: b.Version(),
			OccurredAt:     request.RequestedAt.Format(time.RFC3339Nano),
			RequestID:      request.RequestID.String(),
			Payload: BookingActivatedEvent{
				CheckInID:      request.CheckInID.String(),
				CheckInVersion: request.CheckInVersion,
				CausationID:    request.CorrelationID.String(),
			},
		}
		body, err := json.Marshal(baseEvent)
		if err != nil {
			return nil, err
		}
		headers := map[string]string{
			"x-correlation-id": correlationID.String(),
			"x-app-name":       "booking",
			"x-event-type":     Type,
		}
		headersRaw, err := json.Marshal(headers)
		if err != nil {
			return nil, err
		}
		return append(
			batch,
			event.Event{
				EntityID: b.ID().String(),
				Key:      event.Raw(b.ID().String()),
				Body:     body,
				Headers:  headersRaw,
				Schema:   config.BookingEventsTopic,
			},
		), nil
	}
}
