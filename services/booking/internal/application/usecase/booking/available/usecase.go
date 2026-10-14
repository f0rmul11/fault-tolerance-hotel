package available

import (
	"context"

	"github.com/gofrs/uuid"

	"booking/internal/domain/vo"
	roomdto "booking/internal/dto/room"
	app "booking/internal/pkg/bookingerror"
)

type Rooms interface {
	List(context.Context, uuid.UUID) ([]roomdto.Room, error)
}

type Storage interface {
	FreeRooms(context.Context, []int64, vo.Period) ([]int64, error)
}

type UseCase struct {
	rooms   Rooms
	storage Storage
}

func New(rooms Rooms, storage Storage) *UseCase {
	return &UseCase{
		rooms:   rooms,
		storage: storage,
	}
}

func (s *UseCase) Execute(ctx context.Context, input Input) ([]int64, error) {
	if input.HotelID == uuid.Nil {
		return nil, app.NewInvalidArgumentErr("Не указан отель")
	}
	if err := input.Period.Validate(); err != nil {
		return nil, err
	}

	rooms, err := s.rooms.List(ctx, input.HotelID)
	if err != nil {
		return nil, err
	}
	roomIDs := make([]int64, 0, len(rooms))
	for _, room := range rooms {
		if room.Limits.Allows(input.Period) {
			roomIDs = append(roomIDs, room.ID)
		}
	}
	if len(roomIDs) == 0 {
		return roomIDs, nil
	}

	return s.storage.FreeRooms(ctx, roomIDs, input.Period)
}
