package booking_confirmed

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gofrs/uuid"

	"booking/internal/domain/entity"
)

func TestCorrelationIdentity(t *testing.T) {
	b := entity.With(uuid.Must(uuid.NewV4()),
		entity.WithStatus(entity.Confirmed),
		entity.WithVersion(1))
	batch, err := New(b)(context.Background(), nil)
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
