package register

import (
	"context"

	"github.com/gofrs/uuid"

	"check-in/internal/domain/entity"
)

type Storage interface {
	Create(context.Context, *entity.CheckIn) error
}

type UseCase struct{ storage Storage }

func New(storage Storage) *UseCase {
	return &UseCase{storage: storage}
}

func (s *UseCase) Execute(ctx context.Context, input Input) error {
	id, err := uuid.NewV4()
	if err != nil {
		return err
	}
	c, err := entity.NewCheckIn(id, input.BookingID, input.RoomID, input.GuestID, input.Period)
	if err != nil {
		return err
	}
	c.SetRoomDetails(input.RoomNumber, input.RoomType)

	return s.storage.Create(ctx, c)
}
