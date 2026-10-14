package booking_confirmed

// BookingConfirmedEvent — wire-модель; доменные типы не содержат JSON-тегов.
type BookingConfirmedEvent struct {
	ID         string `json:"id"`
	RoomID     string `json:"room_id"`
	RoomNumber string `json:"room_number"`
	RoomType   string `json:"room_type"`
	StartsAt   string `json:"starts_at"`
	EndsAt     string `json:"ends_at"`
	CreatedAt  string `json:"created_at"`
	GuestID    string `json:"guest_id"`
	Version    int    `json:"version"`
	Status     string `json:"status"`
}
