package get

import (
	"context"

	entity "hotels/internal/domain/entity/room"
	app "hotels/internal/pkg/hotelserror"
	"hotels/internal/pkg/telemetry/traces"
)

type Storage interface {
	Get(context.Context, int64) (*entity.Room, error)
}

type UseCase struct {
	storage Storage
}

func New(storage Storage) *UseCase {
	return &UseCase{storage: storage}
}

func (s *UseCase) Execute(ctx context.Context, id int64) (*entity.Room, error) {
	ctx, span := traces.Start(ctx, "usecase.room.get")
	defer span.End()

	if id <= 0 {
		return nil, app.NewInvalidArgumentErr("Идентификатор номера должен быть положительным числом")
	}
	return s.storage.Get(ctx, id)
}
