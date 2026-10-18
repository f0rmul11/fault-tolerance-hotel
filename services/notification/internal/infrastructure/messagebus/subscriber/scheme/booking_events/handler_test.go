package booking_events

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/IBM/sarama"
	"github.com/gofrs/uuid"

	"notification/internal/application/usecase/notification/activated"
	"notification/internal/application/usecase/notification/canceled"
	"notification/internal/application/usecase/notification/confirmed"
	"notification/internal/dto/push"
	"notification/internal/infrastructure/handlers/booking_activated"
	"notification/internal/infrastructure/handlers/booking_canceled"
	"notification/internal/infrastructure/handlers/booking_confirmed"
	event_router "notification/internal/pkg/event-router"
)

type senderFake struct {
	messages []push.Message
	err      error
}

func (s *senderFake) Send(_ context.Context, message push.Message) error {
	s.messages = append(s.messages, message)
	return s.err
}

func TestNotifications(t *testing.T) {
	for _, test := range []struct {
		event string
		title string
		text  string
	}{
		{
			event: "booking.confirmed.v1",
			title: "Бронирование подтверждено",
			text:  "Номер 101 (Standard) забронирован с 14.10.2026 12:00 +03:00 до 15.10.2026 12:00 +03:00",
		},
		{
			event: "booking.activated.v1",
			title: "Добро пожаловать!",
			text:  "Заселение оформлено",
		},
		{
			event: "booking.canceled.v1",
			title: "Бронирование отменено",
			text:  "отменена",
		},
		{
			event: "booking.checkin-rejected.v1",
		},
	} {
		t.Run(test.event, func(t *testing.T) {
			sender := &senderFake{}
			handler := newHandler(sender)
			message := newMessage(t, test.event)
			if err := handler.Handle(context.Background(), nil, message); err != nil {
				t.Fatal(err)
			}
			if test.title == "" {
				if len(sender.messages) != 0 {
					t.Fatal("unsupported event sent a notification")
				}
				return
			}
			if len(sender.messages) != 1 {
				t.Fatal("one event should send one notification")
			}
			for _, sent := range sender.messages {
				if sent.Title != test.title || !strings.Contains(sent.Text, test.text) || !strings.Contains(sent.Text, sent.BookingID.String()) {
					t.Fatalf("unexpected notification: %+v", sent)
				}
				if sent.BookingID.String() != string(message.Key) || sent.CorrelationID.String() != string(message.Headers[0].Value) {
					t.Fatal("event identity lost")
				}
			}
			// Без inbox повтор доставляется в gateway повторно с тем же correlation-id.
			if err := handler.Handle(context.Background(), nil, message); err != nil || len(sender.messages) != 2 {
				t.Fatalf("unexpected replay: %v", err)
			}
			failure := errors.New("провайдер недоступен")
			sender.err = failure
			if err := handler.Handle(context.Background(), nil, message); !errors.Is(err, failure) {
				t.Fatalf("provider error lost: %v", err)
			}
		})
	}
}

func TestMalformedMessageDoesNotSend(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*sarama.ConsumerMessage)
	}{
		{
			name: "invalid JSON",
			mutate: func(m *sarama.ConsumerMessage) {
				m.Value = []byte("{")
			},
		},
		{
			name: "missing correlation header",
			mutate: func(m *sarama.ConsumerMessage) {
				m.Headers = nil
			},
		},
		{
			name: "different correlation header",
			mutate: func(m *sarama.ConsumerMessage) {
				m.Headers[0].Value = []byte(uuid.Must(uuid.NewV4()).String())
			},
		},
		{
			name: "invalid timestamp",
			mutate: func(m *sarama.ConsumerMessage) {
				m.Value = []byte(strings.ReplaceAll(string(m.Value), "2026-10-14T12:00:00+03:00", "invalid"))
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			sender := &senderFake{}
			message := newMessage(t, "booking.confirmed.v1")
			test.mutate(message)
			if err := newHandler(sender).Handle(context.Background(), nil, message); err == nil || len(sender.messages) != 0 {
				t.Fatalf("invalid event reached provider: %v", err)
			}
		})
	}
}

func newHandler(sender *senderFake) *MessageHandler {
	router := event_router.NewEventRouter[string, []byte](event_router.WithSkipUnknownEventType[string, []byte]())
	router.RegisterAll(
		booking_confirmed.NewHandler(confirmed.New(sender)),
		booking_activated.NewHandler(activated.New(sender)),
		booking_canceled.NewHandler(canceled.New(sender)),
	)
	return NewMessageHandler(router)
}

func newMessage(t *testing.T, eventType string) *sarama.ConsumerMessage {
	t.Helper()
	bookingID := uuid.Must(uuid.NewV4()).String()
	correlationID := uuid.Must(uuid.NewV4()).String()
	body, err := json.Marshal(map[string]any{
		"event_type":     eventType,
		"booking_id":     bookingID,
		"correlation_id": correlationID,
		"payload": map[string]any{
			"room_id":     "1",
			"room_number": "101",
			"room_type":   "Standard",
			"starts_at":   "2026-10-14T12:00:00+03:00",
			"ends_at":     "2026-10-15T12:00:00+03:00",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &sarama.ConsumerMessage{
		Key:   []byte(bookingID),
		Value: body,
		Headers: []*sarama.RecordHeader{{
			Key:   []byte(correlationIDHeader),
			Value: []byte(correlationID),
		}},
	}
}
