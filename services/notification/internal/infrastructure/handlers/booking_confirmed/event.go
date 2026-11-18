package booking_confirmed

import "time"

type confirmationPayload struct {
	RoomNumber string    `json:"room_number"`
	RoomType   string    `json:"room_type"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
}
