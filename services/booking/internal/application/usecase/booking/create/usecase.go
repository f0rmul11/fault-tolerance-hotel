package create

import (
	"context"
	"time"

	"github.com/gofrs/uuid"

	"booking/internal/domain/entity"
	"booking/internal/domain/vo"
	roomdto "booking/internal/dto/room"
	app "booking/internal/pkg/bookingerror"
	"booking/internal/pkg/telemetry/traces"
)

type Storage interface {
	FindByKey(context.Context, uuid.UUID, uuid.UUID) (*entity.Booking, error)
}

type Rooms interface {
	Get(context.Context, int64) (roomdto.Room, error)
}

type Confirmer interface {
	Confirm(context.Context, *entity.Booking, uuid.UUID, vo.Limits) (*entity.Booking, error)
}

type UseCase struct {
	storage   Storage
	rooms     Rooms
	confirmer Confirmer
}

func New(storage Storage, rooms Rooms, confirmer Confirmer) *UseCase {
	return &UseCase{
		storage:   storage,
		rooms:     rooms,
		confirmer: confirmer,
	}
}

func (s *UseCase) Execute(ctx context.Context, input Input) (*entity.Booking, error) {
	ctx, span := traces.Start(ctx, "usecase.booking.create")
	defer span.End()

	if input.IdempotencyKey == uuid.Nil {
		return nil, app.NewInvalidArgumentErr("Не указан ключ идемпотентности")
	}

	id, err := uuid.NewV4()
	if err != nil {
		return nil, err
	}

	booking, err := entity.NewBooking(id, input.Context.ActorID, input.RoomID,
		input.Context.RequestID, input.Period, time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}

	existing, err := s.storage.FindByKey(ctx, booking.GuestID(), input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing.Replay(booking)
	}

	room, err := s.rooms.Get(ctx, booking.RoomID())
	if err != nil {
		return nil, err
	}

	booking.SetRoomDetails(room.Number, room.RoomType)

	return s.confirmer.Confirm(ctx, booking, input.IdempotencyKey, room.Limits)
}
