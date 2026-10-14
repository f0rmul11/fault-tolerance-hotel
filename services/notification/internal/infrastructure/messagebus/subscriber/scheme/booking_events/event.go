package booking_events

const correlationIDHeader = "x-correlation-id"

type BaseEvent struct {
	EventType     string `json:"event_type"`
	EntityID      string `json:"aggregate_id"`
	CorrelationID string `json:"correlation_id"`
}
