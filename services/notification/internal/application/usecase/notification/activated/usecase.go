package activated

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
		Title:         "Добро пожаловать!",
		Text:          fmt.Sprintf("Бронь %s активирована. Заселение оформлено — приятного отдыха!", input.BookingID),
	})
}
