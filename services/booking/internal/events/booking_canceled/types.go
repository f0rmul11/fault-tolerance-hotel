package booking_canceled

type BookingCanceledEvent struct {
	CheckInID      string `json:"checkin_id"`
	CheckInVersion int    `json:"checkin_version"`
	CausationID    string `json:"causation_id"`
}
