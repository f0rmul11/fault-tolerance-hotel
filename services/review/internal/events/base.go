package events

type Base[T any] struct {
	CorrelationID string `json:"correlation_id"`
	EventType     string `json:"event_type"`
	HotelID       string `json:"hotel_id"`
	RatingVersion int64  `json:"rating_version"`
	OccurredAt    string `json:"occurred_at"`

	Payload T `json:"payload"`
}
