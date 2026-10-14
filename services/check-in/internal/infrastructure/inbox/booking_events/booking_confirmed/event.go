package booking_confirmed

import "encoding/json"

type BookingConfirmed struct {
	ID         string `json:"id"`
	RoomID     string `json:"room_id"`
	RoomNumber string `json:"room_number"`
	RoomType   string `json:"room_type"`
	GuestID    string `json:"guest_id"`
	StartsAt   string `json:"starts_at"`
	EndsAt     string `json:"ends_at"`
}

func deserialize(body []byte) (BookingConfirmed, error) {
	var envelope struct {
		Payload BookingConfirmed `json:"payload"`
	}
	err := json.Unmarshal(body, &envelope)
	return envelope.Payload, err
}
