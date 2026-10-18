package booking_confirmed

import "encoding/json"

type BookingConfirmed struct {
	RoomID     string `json:"room_id"`
	RoomNumber string `json:"room_number"`
	RoomType   string `json:"room_type"`
	GuestID    string `json:"guest_id"`
	StartsAt   string `json:"starts_at"`
	EndsAt     string `json:"ends_at"`
}

type Envelope struct {
	BookingID string           `json:"booking_id"`
	Payload   BookingConfirmed `json:"payload"`
}

func deserialize(body []byte) (Envelope, error) {
	var envelope Envelope
	err := json.Unmarshal(body, &envelope)
	return envelope, err
}
