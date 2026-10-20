package checkin_events

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"booking/internal/pkg/inbox/message"
	"testing"

	"github.com/IBM/sarama"
)

func TestEventRequiresCorrelationID(t *testing.T) {
	const id = "de3dab97-8166-4908-97ab-ce3e20f7df75"
	for _, tc := range []struct{ name, body, header string }{
		{"offset is not a key", `{"booking_id":"booking"}`, ""},
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

type inboxFake struct {
	messages message.Messages
	err      error
}

func (i *inboxFake) SaveMessages(_ context.Context, messages message.Messages) error {
	i.messages = messages
	return i.err
}

func TestKafkaHeadersReachInbox(t *testing.T) {
	const correlationID = "de3dab97-8166-4908-97ab-ce3e20f7df75"
	failure := errors.New("inbox unavailable")
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "all headers saved"},
		{name: "storage failure returned", err: failure},
	} {
		t.Run(test.name, func(t *testing.T) {
			inbox := &inboxFake{err: test.err}
			headers := map[string]string{
				"x-correlation-id": correlationID,
				"x-app-name":       "source-service",
				"x-event-type":     "booking.confirmed.v1",
				"traceparent":      "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
				"x-custom":         "Произвольный заголовок",
				"x-empty":          "",
			}
			incoming := &sarama.ConsumerMessage{
				Topic:     "events.v1",
				Key:       []byte("booking"),
				Value:     []byte(`{"booking_id":"booking","correlation_id":"` + correlationID + `"}`),
				Timestamp: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
			}
			for key, value := range headers {
				incoming.Headers = append(incoming.Headers, &sarama.RecordHeader{
					Key:   []byte(key),
					Value: []byte(value),
				})
			}
			err := NewMessageHandler(inbox).Handle(t.Context(), nil, incoming)
			if !errors.Is(err, test.err) {
				t.Fatalf("storage error lost: %v", err)
			}
			if len(inbox.messages) != 1 {
				t.Fatalf("expected one inbox message, got %d", len(inbox.messages))
			}
			for _, saved := range inbox.messages {
				var actual map[string]string
				if err := json.Unmarshal(saved.GetHeaders(), &actual); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(actual, headers) {
					t.Fatalf("headers lost: got %v, want %v", actual, headers)
				}
				if saved.GetCorrelationID() != correlationID || saved.GetTopic() != incoming.Topic || !saved.GetCreatedAt().Equal(incoming.Timestamp) {
					t.Fatal("message identity or timestamp changed")
				}
				if string(saved.GetKey()) != string(incoming.Key) || string(saved.GetBody()) != string(incoming.Value) {
					t.Fatal("message key or body changed")
				}
			}
		})
	}
}
