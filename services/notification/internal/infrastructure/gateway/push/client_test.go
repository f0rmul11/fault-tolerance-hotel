package push

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/gofrs/uuid"

	"notification/internal/dto/push"
)

func TestSendStructuredNotification(t *testing.T) {
	var output bytes.Buffer
	message := push.Message{
		CorrelationID: uuid.Must(uuid.NewV4()),
		BookingID:     uuid.Must(uuid.NewV4()),
		Title:         "Добро пожаловать!",
		Text:          "Заселение оформлено — приятного отдыха!",
	}
	client := NewClient(slog.New(slog.NewJSONHandler(&output, nil)))
	if err := client.Send(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{
		"msg":            message.Title,
		"text":           message.Text,
		"booking_id":     message.BookingID.String(),
		"correlation_id": message.CorrelationID.String(),
		"service":        "notification",
		"provider":       "stdout",
		"channel":        "push",
	} {
		if record[key] != expected {
			t.Fatalf("%s: got %v, want %s", key, record[key], expected)
		}
	}
}
