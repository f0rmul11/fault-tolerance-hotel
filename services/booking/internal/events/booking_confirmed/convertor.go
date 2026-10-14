package booking_confirmed

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/gofrs/uuid"

	"booking/config"
	entity "booking/internal/domain/entity"
	"booking/internal/events"
	app "booking/internal/pkg/bookingerror"
	"booking/internal/pkg/event"
	"booking/internal/pkg/pipe"
)

const Type = "booking.confirmed.v1"

func New(b *entity.Booking) pipe.Func[event.Events] {
	return func(ctx context.Context, batch event.Events) (event.Events, error) {
		if b.Status() != entity.Confirmed {
			return nil, app.NewInternalError(
				"Нельзя сформировать событие неподтверждённой брони",
				nil,
			)
		}

		correlationID, err := uuid.NewV4()
		if err != nil {
			return nil, err
		}

		baseEvent := events.Base[BookingConfirmedEvent]{
			CorrelationID:    correlationID.String(),
			EventType:        Type,
			EntityID:         b.ID().String(),
			AggregateVersion: b.Version(),
			OccurredAt:       b.UpdatedAt().Format(time.RFC3339Nano),
			RequestID:        b.RequestID().String(),
			Payload: BookingConfirmedEvent{
				ID:         b.ID().String(),
				RoomID:     strconv.FormatInt(b.RoomID(), 10),
				RoomNumber: b.RoomNumber(),
				RoomType:   b.RoomType(),
				StartsAt:   b.Period().Start().Format(time.RFC3339Nano),
				EndsAt:     b.Period().End().Format(time.RFC3339Nano),
				CreatedAt:  b.CreatedAt().Format(time.RFC3339Nano),
				GuestID:    b.GuestID().String(),
				Version:    b.Version(),
				Status:     string(b.Status()),
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

		return append(batch, event.Event{
			EntityID: b.ID().String(),
			Key:      event.Raw(b.ID().String()),
			Body:     body,
			Headers:  headersRaw,
			Schema:   config.BookingEventsTopic,
		}), nil
	}
}
