package enrich

import (
	"context"

	"github.com/gofrs/uuid"

	entity "hotels/internal/domain/entity/hotel"
	dto "hotels/internal/dto/hotel"
	"hotels/internal/pkg/telemetry/traces"
)

type Review interface {
	GetHotelRatings(context.Context, []uuid.UUID) (map[uuid.UUID]dto.Rating, error)
}

type Service struct {
	review Review
}

func New(review Review) *Service {
	return &Service{review: review}
}

func (s *Service) Hotels(ctx context.Context, hotels []*entity.Hotel) error {
	ctx, span := traces.Start(ctx, "service.hotel.enrich.Hotels")
	defer span.End()

	if len(hotels) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(hotels))
	for _, hotel := range hotels {
		ids = append(ids, hotel.ID())
	}
	ratings, err := s.review.GetHotelRatings(ctx, ids)
	if err != nil {
		return err
	}
	for _, hotel := range hotels {
		hotel.SetRating(ratings[hotel.ID()])
	}
	return nil
}
