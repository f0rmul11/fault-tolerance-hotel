package get

import (
	"context"

	"github.com/gofrs/uuid"

	entity "hotels/internal/domain/entity/hotel"
	app "hotels/internal/pkg/hotelserror"
	"hotels/internal/pkg/telemetry/traces"
)

type Adapter interface {
	GetWithSoftTTL(context.Context, uuid.UUID, int64) (*entity.Hotel, error)
}

type UseCase struct {
	adapter Adapter
}

func New(adapter Adapter) *UseCase {
	return &UseCase{
		adapter: adapter,
	}
}

func (s *UseCase) Execute(ctx context.Context, input Input) (*entity.Hotel, error) {
	ctx, span := traces.Start(ctx, "usecase.hotel.get")
	defer span.End()

	if input.HotelID == uuid.Nil {
		return nil, app.NewInvalidArgumentErr("Некорректный идентификатор отеля")
	}

	if input.MinRatingVersion < 0 {
		return nil, app.NewInvalidArgumentErr("Минимальная версия рейтинга не может быть отрицательной")
	}

	// Адаптер возвращает полный снимок, уже обогащённый рейтингом.
	return s.adapter.GetWithSoftTTL(ctx, input.HotelID, input.MinRatingVersion)
}
