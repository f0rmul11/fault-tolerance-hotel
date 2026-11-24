package review_events

type BaseEvent struct {
	EventType     string `json:"event_type"`
	CorrelationID string `json:"correlation_id"`
}
