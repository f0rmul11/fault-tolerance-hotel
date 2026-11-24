package rating_changed

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gofrs/uuid"

	"review/config"
	entity "review/internal/domain/entity/rating"
	"review/internal/events"
	"review/internal/pkg/event"
	"review/internal/pkg/pipe"
	"review/internal/pkg/telemetry/traces"
)

const Type = "review.rating.changed.v1"

func New(rating *entity.Rating) pipe.Func[event.Events] {
	return func(ctx context.Context, batch event.Events) (event.Events, error) {
		correlationID, err := uuid.NewV4()
		if err != nil {
			return nil, err
		}
		envelope := events.Base[RatingChanged]{
			CorrelationID: correlationID.String(),
			EventType:     Type,
			HotelID:       rating.ID().String(),
			RatingVersion: rating.Version(),
			OccurredAt:    rating.UpdatedAt().Format(time.RFC3339Nano),
			Payload:       RatingChanged{},
		}
		body, err := json.Marshal(envelope)
		if err != nil {
			return nil, err
		}
		headers := map[string]string{
			"x-correlation-id": correlationID.String(),
			"x-app-name":       "review",
			"x-event-type":     Type,
		}
		traces.Inject(ctx, headers)
		rawHeaders, err := json.Marshal(headers)
		if err != nil {
			return nil, err
		}
		return append(batch, event.Event{
			EntityID: rating.ID().String(),
			Key:      event.Raw(rating.ID().String()),
			Body:     body,
			Headers:  rawHeaders,
			Schema:   config.RatingEventsTopic,
		}), nil
	}
}
