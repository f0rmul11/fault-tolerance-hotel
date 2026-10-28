package list

import (
	"context"

	"github.com/gofrs/uuid"

	runtimeconfig "hotels/internal/config"
	entity "hotels/internal/domain/entity/room"
	app "hotels/internal/pkg/hotelserror"
	"hotels/internal/pkg/telemetry/traces"
)

type Storage interface {
	List(context.Context, uuid.UUID) ([]*entity.Room, error)
}

type UseCase struct {
	storage  Storage
	rtConfig *runtimeconfig.Client
}

func New(storage Storage, rtConfig *runtimeconfig.Client) *UseCase {
	return &UseCase{
		storage:  storage,
		rtConfig: rtConfig,
	}
}

func (s *UseCase) Execute(ctx context.Context, id uuid.UUID) ([]*entity.Room, error) {
	ctx, span := traces.Start(ctx, "usecase.room.list")
	defer span.End()

	if id == uuid.Nil {
		return nil, app.NewInvalidArgumentErr("Не указан идентификатор")
	}

	if !s.rtConfig.GetValue(ctx, runtimeconfig.RoomsListEnabled).Bool() {
		return nil, app.NewBusinessErr("Получение списка номеров отключено")
	}

	return s.storage.List(ctx, id)
}
