package confirmed

import (
	"context"
	"fmt"

	"notification/internal/dto/push"
	"notification/internal/pkg/telemetry/traces"
)

type Sender interface {
	Send(context.Context, push.Message) error
}

type UseCase struct {
	push Sender
}

func New(push Sender) *UseCase {
	return &UseCase{
		push: push,
	}
}

func (s *UseCase) Execute(ctx context.Context, input Input) error {
	ctx, span := traces.Start(ctx, "usecase.notification.confirmed")
	defer span.End()

	return s.push.Send(ctx, push.Message{
		CorrelationID: input.CorrelationID,
		BookingID:     input.BookingID,
		Title:         "Бронирование подтверждено",
		Text: fmt.Sprintf(
			"Номер %s (%s) забронирован с %s до %s. Номер брони: %s. Ждём вас!",
			input.RoomNumber,
			input.RoomType,
			input.StartsAt.Format("02.01.2006 15:04 -07:00"),
			input.EndsAt.Format("02.01.2006 15:04 -07:00"),
			input.BookingID,
		),
	})
}
