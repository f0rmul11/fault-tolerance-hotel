package booking_events

import (
	"encoding/json"
	"errors"

	"github.com/gofrs/uuid"

	"notification/internal/dto/bookingevent"
)

const correlationIDHeader = "x-correlation-id"

func decodeEvent(body []byte, headers map[string]string) (bookingevent.Event, error) {
	var event struct {
		Type          string          `json:"event_type"`
		BookingID     string          `json:"booking_id"`
		CorrelationID string          `json:"correlation_id"`
		Payload       json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		return bookingevent.Event{}, errors.New("invalid booking event JSON")
	}

	if event.Type == "" {
		return bookingevent.Event{}, errors.New("missing event type")
	}

	if eventType, ok := headers["x-event-type"]; ok && eventType != event.Type {
		return bookingevent.Event{}, errors.New("event type in header and body differ")
	}

	correlationHeader := headers[correlationIDHeader]
	correlationID := uuid.FromStringOrNil(correlationHeader)
	if correlationID == uuid.Nil {
		return bookingevent.Event{}, errors.New("missing or invalid x-correlation-id")
	}

	if correlationHeader != event.CorrelationID {
		return bookingevent.Event{}, errors.New("correlation ID in header and body differ")
	}

	bookingID := uuid.FromStringOrNil(event.BookingID)
	if bookingID == uuid.Nil {
		return bookingevent.Event{}, errors.New("missing or invalid booking ID")
	}

	return bookingevent.Event{
		Type:          event.Type,
		BookingID:     bookingID,
		CorrelationID: correlationID,
		Payload:       event.Payload,
	}, nil
}
