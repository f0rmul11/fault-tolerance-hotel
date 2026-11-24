package rating_changed

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"review/config"
	entity "review/internal/domain/entity/rating"
	"review/internal/events"
)

func TestEventIdentityAndSnapshot(t *testing.T) {
	id := uuid.Must(uuid.NewV4())
	rating := entity.With(id, entity.WithTotalStars(42), entity.WithReviewCount(10), entity.WithVersion(7))
	if err := rating.Add(5, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	batch, err := New(rating)(t.Context(), nil)
	if err != nil || len(batch) != 1 {
		t.Fatalf("event: %v", err)
	}
	var body events.Base[RatingChanged]
	var headers map[string]string
	if err := json.Unmarshal(batch[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(batch[0].Headers, &headers); err != nil {
		t.Fatal(err)
	}
	if body.EventType != Type || body.HotelID != id.String() || body.RatingVersion != 8 || body.OccurredAt != rating.UpdatedAt().Format(time.RFC3339Nano) {
		t.Fatalf("invalid body: %+v", body)
	}
	if uuid.FromStringOrNil(body.CorrelationID) == uuid.Nil || headers["x-correlation-id"] != body.CorrelationID || headers["x-app-name"] != "review" || headers["x-event-type"] != Type {
		t.Fatalf("invalid identity: %+v", headers)
	}
	if batch[0].Schema != config.RatingEventsTopic || string(batch[0].Key) != id.String() || batch[0].EntityID != id.String() {
		t.Fatal("invalid routing")
	}
	var wire map[string]any
	if err := json.Unmarshal(batch[0].Body, &wire); err != nil {
		t.Fatal(err)
	}
	if _, exists := wire["request_id"]; exists {
		t.Fatal("request_id must not be added")
	}
}
