package canceled

import (
	"context"
	"fmt"

	"notification/internal/dto/push"
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
	return s.push.Send(ctx, push.Message{
		CorrelationID: input.CorrelationID,
		BookingID:     input.BookingID,
		Title:         "Бронирование отменено",
		Text:          fmt.Sprintf("Бронь %s отменена. Будем рады видеть вас снова!", input.BookingID),
	})
}
