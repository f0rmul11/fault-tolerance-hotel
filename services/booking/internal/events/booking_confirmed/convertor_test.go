package booking_confirmed

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gofrs/uuid"

	entity "booking/internal/domain/entity"
	"booking/internal/domain/vo"
	"booking/internal/events"
)

func TestEventCarriesRoomAndPeriod(t *testing.T) {
	at := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	period, _ := vo.NewPeriod(at, at.Add(12*time.Hour))
	id := uuid.Must(uuid.NewV4())
	roomID := int64(1)
	requestID := uuid.Must(uuid.NewV4())
	b, err := entity.NewBooking(id, uuid.Must(uuid.NewV4()), roomID, requestID, period, at)
	if err != nil {
		t.Fatal(err)
	}

	b.SetRoomDetails("101", "Standard")

	batch, err := New(b)(context.Background(), nil)
	if err != nil || len(batch) != 1 {
		t.Fatal(err)
	}

	var wire struct {
		BookingID      string                     `json:"booking_id"`
		BookingVersion int                        `json:"booking_version"`
		Payload        map[string]json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(batch[0].Body, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.BookingID != id.String() || wire.BookingVersion != b.Version() {
		t.Fatal("wire contract lost booking identity or version")
	}
	for _, key := range []string{"id", "booking_id", "version", "booking_version", "request_id"} {
		if _, exists := wire.Payload[key]; exists {
			t.Fatalf("payload duplicates envelope field %s", key)
		}
	}

	var envelope events.Base[BookingConfirmedEvent]
	if err := json.Unmarshal(batch[0].Body, &envelope); err != nil {
		t.Fatal(err)
	}

	if envelope.EventType != Type || envelope.RequestID != requestID.String() {
		t.Fatal("event lost type or request identity")
	}
	if envelope.BookingID != id.String() || string(batch[0].Key) != id.String() {
		t.Fatal("Kafka booking ID and key must identify the booking UUID")
	}
	if envelope.Payload.RoomID != "1" || envelope.Payload.RoomNumber != "101" || envelope.Payload.RoomType != "Standard" {
		t.Fatal("event lost room identity")
	}
	if envelope.Payload.StartsAt != period.Start().Format(time.RFC3339Nano) ||
		envelope.Payload.EndsAt != period.End().Format(time.RFC3339Nano) {
		t.Fatal("event changed the period")
	}
}
