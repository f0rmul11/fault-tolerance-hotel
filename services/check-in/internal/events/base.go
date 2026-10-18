package events

// Base сохраняет внешний контракт HotelFlow; Payload принадлежит пакету события.
type Base[T any] struct {
	CorrelationID string `json:"correlation_id"`
	EventType     string `json:"event_type"`
	BookingID     string `json:"booking_id"`
	OccurredAt    string `json:"occurred_at"`
	RequestID     string `json:"request_id"`
	Payload       T      `json:"payload"`
}
