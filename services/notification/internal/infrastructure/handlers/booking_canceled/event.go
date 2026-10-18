package booking_canceled

import (
	"encoding/json"
)

type Event struct {
	CorrelationID string `json:"correlation_id"`
	BookingID     string `json:"booking_id"`
}

func deserialize(body []byte) (Event, error) {
	var event Event
	err := json.Unmarshal(body, &event)
	return event, err
}
