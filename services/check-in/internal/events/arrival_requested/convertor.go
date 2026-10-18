package arrival_requested

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gofrs/uuid"

	"check-in/config"
	"check-in/internal/domain/entity"
	"check-in/internal/events"
	"check-in/internal/pkg/event"
	"check-in/internal/pkg/pipe"
)

const Type = "checkin.arrival-requested.v1"

func New(c *entity.CheckIn) pipe.Func[event.Events] {
	return func(ctx context.Context, batch event.Events) (event.Events, error) {
		correlationID, err := uuid.NewV4()
		if err != nil {
			return nil, err
		}
		baseEvent := events.Base[ArrivalRequestedEvent]{
			CorrelationID: correlationID.String(),
			EventType:     Type,
			BookingID:     c.BookingID().String(),
			OccurredAt:    c.RequestedAt().Format(time.RFC3339Nano),
			RequestID:     c.RequestID().String(),
			Payload: ArrivalRequestedEvent{
				CheckInID:      c.ID().String(),
				CheckInVersion: c.Version(),
				ActorID:        c.ActorID().String(),
				RequestedAt:    c.RequestedAt(),
			},
		}
		body, err := json.Marshal(baseEvent)
		if err != nil {
			return nil, err
		}
		headers := map[string]string{
			"x-correlation-id": correlationID.String(),
			"x-app-name":       "check-in",
			"x-event-type":     Type,
		}
		headersRaw, err := json.Marshal(headers)
		if err != nil {
			return nil, err
		}
		return append(
			batch,
			event.Event{
				EntityID: c.BookingID().String(),
				Key:      event.Raw(c.BookingID().String()),
				Body:     body,
				Headers:  headersRaw,
				Schema:   config.CheckinEventsTopic,
			},
		), nil
	}
}
