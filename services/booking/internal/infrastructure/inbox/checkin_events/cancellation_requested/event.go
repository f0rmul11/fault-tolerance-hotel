package cancellation_requested

import (
	"encoding/json"
	"time"
)

type Request struct {
	CheckInID      string    `json:"checkin_id"`
	BookingID      string    `json:"booking_id"`
	CheckInVersion int       `json:"checkin_version"`
	ActorID        string    `json:"actor_id"`
	RequestID      string    `json:"request_id"`
	RequestedAt    time.Time `json:"requested_at"`
}

type Envelope struct {
	CorrelationID string  `json:"correlation_id"`
	Payload       Request `json:"payload"`
}

func deserialize(body []byte) (Envelope, error) {
	var e Envelope
	err := json.Unmarshal(body, &e)
	return e, err
}
