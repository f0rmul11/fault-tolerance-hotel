package rating_changed

import (
	"context"

	"github.com/gofrs/uuid"

	"hotels/internal/application/usecase/hotel/refresh"
	"hotels/internal/pkg/telemetry/traces"
)

const eventType = "review.rating.changed.v1"

type Handler struct {
	usecase *refresh.UseCase
}

func NewHandler(usecase *refresh.UseCase) *Handler {
	return &Handler{
		usecase: usecase,
	}
}

func (h *Handler) HandleEvent(ctx context.Context, body []byte) (err error) {
	ctx, span := traces.Start(ctx, "handler.event.rating_changed")
	defer span.End()
	defer func() {
		traces.RecordError(span, err)
	}()

	event, err := deserialize(body)
	if err != nil {
		return err
	}

	// Даже при повторной доставке читаем актуальный рейтинг из Review, а не из события.
	return h.usecase.Execute(ctx, uuid.FromStringOrNil(event.HotelID))
}

func (h *Handler) EventType() string {
	return eventType
}
