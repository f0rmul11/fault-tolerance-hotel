package get

import (
	"context"

	"github.com/gofrs/uuid"

	"booking/internal/domain/entity"
	"booking/internal/pkg/bookingerror"
	"booking/internal/pkg/telemetry/traces"
)

type Storage interface {
	Get(context.Context, uuid.UUID) (*entity.Booking, error)
}

type UseCase struct {
	storage Storage
}

func New(storage Storage) *UseCase {
	return &UseCase{
		storage: storage,
	}
}

func (s *UseCase) Execute(ctx context.Context, input Input) (*entity.Booking, error) {
	ctx, span := traces.Start(ctx, "usecase.booking.get")
	defer span.End()

	if input.BookingID == uuid.Nil {
		return nil, bookingerror.NewInvalidArgumentErr("Не указан идентификатор брони")
	}
	booking, err := s.storage.Get(ctx, input.BookingID)
	if err != nil {
		return nil, err
	}
	if input.Context.Role == entity.RoleGuest && booking.GuestID() != input.Context.ActorID {
		return nil, bookingerror.New(
			bookingerror.WithCode(int(bookingerror.PermissionDenied)),
			bookingerror.WithMessage("Бронь принадлежит другому гостю"),
			bookingerror.WithClassification("internal"),
		)
	}
	return booking, nil
}
