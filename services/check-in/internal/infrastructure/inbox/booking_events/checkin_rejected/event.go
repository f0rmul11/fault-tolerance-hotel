package checkin_rejected

import "encoding/json"

type Reply struct {
	CheckInID      string `json:"checkin_id"`
	BookingID      string `json:"booking_id"`
	CheckInVersion int    `json:"checkin_version"`
	Message        string `json:"message"`
}

func deserialize(body []byte) (Reply, error) {
	var envelope struct {
		Payload Reply `json:"payload"`
	}
	err := json.Unmarshal(body, &envelope)
	return envelope.Payload, err
}
