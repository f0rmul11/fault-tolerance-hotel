package arrival_requested

import "time"

type ArrivalRequestedEvent struct {
	CheckInID      string    `json:"checkin_id"`
	CheckInVersion int       `json:"checkin_version"`
	ActorID        string    `json:"actor_id"`
	RequestedAt    time.Time `json:"requested_at"`
}
