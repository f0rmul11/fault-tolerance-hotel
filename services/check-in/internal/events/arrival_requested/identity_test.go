package arrival_requested

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
		entity.WithVersion(2))
	batch, err := New(c)(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		CorrelationID string `json:"correlation_id"`
		EventType     string `json:"event_type"`
	}
	var headers map[string]string
	if err := json.Unmarshal(batch[0].Body, &envelope); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(batch[0].Headers, &headers); err != nil {
		t.Fatal(err)
	}
	if uuid.FromStringOrNil(envelope.CorrelationID) == uuid.Nil || headers["x-correlation-id"] != envelope.CorrelationID || envelope.EventType != Type {
		t.Fatal("event identity is missing or inconsistent")
	}

}
