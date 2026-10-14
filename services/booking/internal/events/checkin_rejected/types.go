package checkin_rejected

type CheckinRejectedEvent struct {
	CheckInID      string `json:"checkin_id"`
	BookingID      string `json:"booking_id"`
	CheckInVersion int    `json:"checkin_version"`
	CausationID    string `json:"causation_id"`
	Message        string `json:"message"`
}
