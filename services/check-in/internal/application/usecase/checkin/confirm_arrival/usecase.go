package confirm_arrival

import (
	"context"

	"github.com/gofrs/uuid"

	"check-in/internal/domain/entity"
	app "check-in/internal/pkg/checkinerror"
	"check-in/internal/pkg/telemetry/traces"
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
	ctx, span := traces.Start(ctx, "usecase.checkin.confirm_arrival")
	defer span.End()

	c, err := s.storage.Get(ctx, input.BookingID)
	if err != nil {
		return err
	}
	if c.ID() != input.CheckInID {
		return app.NewConflictErr("Ответ относится к другому заселению")
	}
	if !c.ConfirmArrival(input.CheckInVersion) {
		return nil
	}
	return s.storage.Save(ctx, c)
}
