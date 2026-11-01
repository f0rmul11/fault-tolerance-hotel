package list

import (
	"booking/internal/domain/entity"
	"context"

	"github.com/gofrs/uuid"
)

type Storage interface {
	List(context.Context, uuid.UUID) (entity.BookingList, error)
}

type UseCase struct {
	storage Storage
}

func New(storage Storage) *UseCase {
	return &UseCase{storage: storage}
}

func (s *UseCase) Execute(ctx context.Context, input Input) (entity.BookingList, error) {
	return s.storage.List(ctx, input.Context.ActorID)
}
