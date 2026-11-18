package booking_confirmed

import (
	"context"
	"encoding/json"

	"notification/internal/application/usecase/notification/confirmed"
	"notification/internal/dto/bookingevent"
	"notification/internal/pkg/notificationerror"
	"notification/internal/pkg/telemetry/traces"
)

const eventType = "booking.confirmed.v1"

type Handler struct {
	usecase *confirmed.UseCase
}

func NewHandler(usecase *confirmed.UseCase) *Handler {
	return &Handler{
		usecase: usecase,
	}
}

func (h *Handler) HandleEvent(ctx context.Context, event bookingevent.Event) (err error) {
	ctx, span := traces.Start(ctx, "handler.event.booking_confirmed")
	defer span.End()
	defer func() {
		traces.RecordError(span, err)
	}()

	var payload confirmationPayload
	if err = json.Unmarshal(event.Payload, &payload); err != nil {
		return notificationerror.NewInvalidArgumentErr("invalid booking confirmation payload")
	}
	if payload.RoomNumber == "" || payload.RoomType == "" {
		return notificationerror.NewInvalidArgumentErr("booking confirmation is missing room number or type")
	}
	if payload.StartsAt.IsZero() || !payload.EndsAt.After(payload.StartsAt) {
		return notificationerror.NewInvalidArgumentErr("invalid booking period")
	}
	return h.usecase.Execute(ctx, confirmed.Input{
		CorrelationID: event.CorrelationID,
		BookingID:     event.BookingID,
		RoomNumber:    payload.RoomNumber,
		RoomType:      payload.RoomType,
		StartsAt:      payload.StartsAt,
		EndsAt:        payload.EndsAt,
	})
}

func (h *Handler) EventType() string {
	return eventType
}
