package booking_canceled

import "encoding/json"

type Reply struct {
	CheckInID      string `json:"checkin_id"`
	CheckInVersion int    `json:"checkin_version"`
}

type Envelope struct {
	BookingID string `json:"booking_id"`
	Payload   Reply  `json:"payload"`
}

func deserialize(body []byte) (Envelope, error) {
	var envelope Envelope
	err := json.Unmarshal(body, &envelope)
	return envelope, err
}
