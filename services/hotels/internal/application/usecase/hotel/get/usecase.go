package get

import (
	"context"

	"github.com/gofrs/uuid"

	entity "hotels/internal/domain/entity/hotel"
	dto "hotels/internal/dto/hotel"
	app "hotels/internal/pkg/hotelserror"
	"hotels/internal/pkg/telemetry/traces"
)

type Storage interface {
	Get(context.Context, uuid.UUID) (*entity.Hotel, error)
}

type Review interface {
	GetHotelRatings(context.Context, []uuid.UUID) (map[uuid.UUID]dto.Rating, error)
}

type UseCase struct {
	storage Storage
	review  Review
}

func New(storage Storage, review Review) *UseCase {
	return &UseCase{
		storage: storage,
		review:  review,
	}
}

func (s *UseCase) Execute(ctx context.Context, id uuid.UUID) (*entity.Hotel, error) {
	ctx, span := traces.Start(ctx, "usecase.hotel.get")
	defer span.End()

	if id == uuid.Nil {
		return nil, app.NewInvalidArgumentErr("Некорректный идентификатор отеля")
	}

	hotel, err := s.storage.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	ratings, err := s.review.GetHotelRatings(ctx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}

	hotel.SetRating(ratings[id])
	return hotel, nil
}
