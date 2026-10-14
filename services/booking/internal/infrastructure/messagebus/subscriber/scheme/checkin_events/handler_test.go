package checkin_events

import (
	"context"
	"testing"

	"github.com/IBM/sarama"
)

func TestEventRequiresCorrelationID(t *testing.T) {
	const id = "de3dab97-8166-4908-97ab-ce3e20f7df75"
	for _, tc := range []struct{ name, body, header string }{
		{"offset is not a key", `{"aggregate_id":"booking"}`, ""},
		{"event_id is not a key", `{"event_id":"` + id + `"}`, ""},
		{"invalid UUID", `{"correlation_id":"bad"}`, "bad"},
		{"header disagrees with body", `{"correlation_id":"39a654bc-aed7-472d-8cc3-535b826f4ef0"}`, id},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewMessageHandler(nil)
			msg := &sarama.ConsumerMessage{
				Value: []byte(tc.body),
				Headers: []*sarama.RecordHeader{{
					Key:   []byte(correlationIDHeader),
					Value: []byte(tc.header),
				}},
			}
			if err := h.Handle(context.Background(), nil, msg); err == nil {
				t.Fatal("event without a valid stable identity was accepted")
			}
		})
	}
}
