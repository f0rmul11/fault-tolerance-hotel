package booking_confirmed

import (
	"encoding/json"
	"time"
)

type Event struct {
	CorrelationID string `json:"correlation_id"`
	BookingID     string `json:"booking_id"`
	Payload       struct {
		RoomNumber string    `json:"room_number"`
		RoomType   string    `json:"room_type"`
		StartsAt   time.Time `json:"starts_at"`
		EndsAt     time.Time `json:"ends_at"`
	} `json:"payload"`
}

func deserialize(body []byte) (Event, error) {
	var event Event
	err := json.Unmarshal(body, &event)
	return event, err
}
