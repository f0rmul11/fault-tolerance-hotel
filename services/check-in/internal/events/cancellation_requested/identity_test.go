package cancellation_requested

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gofrs/uuid"

	"check-in/internal/domain/entity"
)

func TestCorrelationIdentity(t *testing.T) {
	c := entity.With(uuid.Must(uuid.NewV4()),
		entity.WithBookingID(uuid.Must(uuid.NewV4())),
		entity.WithVersion(2),
		entity.WithRequestID(uuid.Must(uuid.NewV4())))
	batch, err := New(c)(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		CorrelationID string `json:"correlation_id"`
		BookingID     string `json:"booking_id"`
		RequestID     string `json:"request_id"`
		Payload       struct {
			CheckInID      string `json:"checkin_id"`
			CheckInVersion int    `json:"checkin_version"`
		} `json:"payload"`
		EventType string `json:"event_type"`
	}
	var headers map[string]string
	if err := json.Unmarshal(batch[0].Body, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.BookingID != c.BookingID().String() || envelope.RequestID != c.RequestID().String() || envelope.Payload.CheckInID != c.ID().String() || envelope.Payload.CheckInVersion != c.Version() {
		t.Fatal("wire contract lost booking, check-in or request identity")
	}

	if err := json.Unmarshal(batch[0].Headers, &headers); err != nil {
		t.Fatal(err)
	}
	if uuid.FromStringOrNil(envelope.CorrelationID) == uuid.Nil || headers["x-correlation-id"] != envelope.CorrelationID || envelope.EventType != Type {
		t.Fatal("event identity is missing or inconsistent")
	}

}
