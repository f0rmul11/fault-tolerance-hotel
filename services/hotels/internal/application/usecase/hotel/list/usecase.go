package list

import (
	"context"

	entity "hotels/internal/domain/entity/hotel"
	app "hotels/internal/pkg/hotelserror"
	"hotels/internal/pkg/telemetry/traces"
)

type Storage interface {
	List(context.Context, int, int) ([]*entity.Hotel, error)
}

type Enricher interface {
	Hotels(context.Context, []*entity.Hotel) error
}

type UseCase struct {
	storage  Storage
	enricher Enricher
}

func New(storage Storage, enricher Enricher) *UseCase {
	return &UseCase{
		storage:  storage,
		enricher: enricher,
	}
}

func (s *UseCase) Execute(ctx context.Context, input Input) ([]*entity.Hotel, error) {
	ctx, span := traces.Start(ctx, "usecase.hotel.list")
	defer span.End()

	limit := input.Limit.OrElse(20)
	if limit < 1 || limit > 100 || input.Offset < 0 {
		return nil, app.NewInvalidArgumentErr("Лимит должен быть от 1 до 100, смещение — неотрицательным")
	}
	hotels, err := s.storage.List(ctx, limit, input.Offset)
	if err != nil {
		return nil, err
	}
	if err := s.enricher.Hotels(ctx, hotels); err != nil {
		return nil, err
	}
	return hotels, nil
}
