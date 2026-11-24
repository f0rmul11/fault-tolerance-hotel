package rating_changed

import "encoding/json"

type Envelope struct {
	HotelID       string `json:"hotel_id"`
	RatingVersion int64  `json:"rating_version"`
}

func deserialize(body []byte) (Envelope, error) {
	var envelope Envelope
	err := json.Unmarshal(body, &envelope)
	return envelope, err
}
