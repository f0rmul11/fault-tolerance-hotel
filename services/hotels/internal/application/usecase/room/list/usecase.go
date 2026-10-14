package list

import (
	"context"

	"github.com/gofrs/uuid"

	entity "hotels/internal/domain/entity/room"
	app "hotels/internal/pkg/hotelserror"
)

type Storage interface {
	List(context.Context, uuid.UUID) ([]*entity.Room, error)
}

type UseCase struct {
	storage Storage
}

func New(storage Storage) *UseCase {
	return &UseCase{storage: storage}
}

func (s *UseCase) Execute(ctx context.Context, id uuid.UUID) ([]*entity.Room, error) {
	if id == uuid.Nil {
		return nil, app.NewInvalidArgumentErr("Не указан идентификатор")
	}
	return s.storage.List(ctx, id)
}
