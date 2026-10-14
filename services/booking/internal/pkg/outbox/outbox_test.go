package outbox

import (
	"booking/internal/pkg/event"
	"booking/internal/pkg/outbox/message"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
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

func TestProcessingErrorsLogOnlyWhenChanged(t *testing.T) {
	var output bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() {
		slog.SetDefault(original)
	})

	queue := NewOutbox(nil, Config{})
	ctx := context.Background()
	queue.logProcessingError(ctx, errors.New("database unavailable"))
	queue.logProcessingError(ctx, errors.New("database unavailable"))
	queue.logProcessingError(ctx, errors.New("commit failed"))
	queue.logProcessingError(ctx, errors.New("commit failed"))
	queue.logProcessingError(ctx, nil)
	queue.logProcessingError(ctx, errors.New("commit failed"))
	if got := strings.Count(output.String(), "Ошибка обработки outbox"); got != 3 {
		t.Fatalf("logs=%d output=%s", got, output.String())
	}
}

func TestConcurrentProcessingErrorsProduceOneLog(t *testing.T) {
	var output bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() {
		slog.SetDefault(original)
	})

	queue := NewOutbox(nil, Config{})
	var workers sync.WaitGroup
	for range 20 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			queue.logProcessingError(context.Background(), errors.New("commit failed"))
		}()
	}
	workers.Wait()
	if got := strings.Count(output.String(), "Ошибка обработки outbox"); got != 1 {
		t.Fatalf("logs=%d output=%s", got, output.String())
	}
}
