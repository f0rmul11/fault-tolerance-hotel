package checkin_events

const correlationIDHeader = "x-correlation-id"

type BaseEvent struct {
	CorrelationID string `json:"correlation_id"`
	EntityID      string `json:"aggregate_id"`
}
