package confirm_cancellation

import (
	"context"

	"github.com/gofrs/uuid"

	"check-in/internal/domain/entity"
	app "check-in/internal/pkg/checkinerror"
)

type Storage interface {
	Get(context.Context, uuid.UUID) (*entity.CheckIn, error)
	Save(context.Context, ...*entity.CheckIn) error
}

type UseCase struct{ storage Storage }

func New(storage Storage) *UseCase {
	return &UseCase{storage: storage}
}

func (s *UseCase) Execute(ctx context.Context, input Input) error {
	c, err := s.storage.Get(ctx, input.BookingID)
	if err != nil {
		return err
	}
	if c.ID() != input.CheckInID {
		return app.NewConflictErr("Ответ относится к другому заселению")
	}
	if !c.ConfirmCancellation(input.CheckInVersion) {
		return nil
	}
	return s.storage.Save(ctx, c)
}
