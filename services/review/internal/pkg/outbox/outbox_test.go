package outbox

import (
	"context"
	"errors"
	"testing"

	"review/internal/pkg/event"
	"review/internal/pkg/outbox/message"
)

type handler struct {
	err      error
	received event.Events
}

func (h *handler) Handle(_ context.Context, events event.Events) error {
	h.received = append(h.received, events...)
	return h.err
}

func (h *handler) Topic() string {
	return "bookings"
}

func (h *handler) BatchSize(context.Context) int {
	return 2
}

func TestHandlerRecordsDeliveryResult(t *testing.T) {
	for _, fail := range []bool{
		false,
		true,
	} {
		t.Run(map[bool]string{
			false: "sent",
			true:  "failed",
		}[fail], func(t *testing.T) {
			handler := &handler{}
			if fail {
				handler.err = errors.New("Kafka unavailable")
			}
			outbox := NewOutbox(nil, Config{
				MaxErrCountForMessage: 10,
			})
			outbox.RegisterHandler(handler)
			msg := message.NewMessage(
				"booking-id",
				handler.Topic(),
				[]byte("key"),
				[]byte("body"),
				message.WithHeaders([]byte(`{"x-event-type":"booking.confirmed.v1"}`)),
			)
			outbox.processMessagesByTopic(context.Background(), handler.Topic(), []*message.Message{msg})
			if len(handler.received) != 1 || string(handler.received[0].Headers) != string(msg.GetHeaders()) {
				t.Fatal("event or headers lost")
			}
			if fail && (msg.GetProcessedAt() != nil || msg.GetErrorsCount() != 1 || msg.GetErrorDesc() == nil) {
				t.Fatal("failed delivery marked processed")
			}
			if !fail && (msg.GetProcessedAt() == nil || msg.GetErrorsCount() != 0) {
				t.Fatal("successful delivery not marked")
			}
		})
	}
}
