package ratings

import (
	"context"
	"log/slog"

	"github.com/gofrs/uuid"

	dto "hotels/internal/dto/hotel"
	app "hotels/internal/pkg/hotelserror"
	"hotels/internal/pkg/telemetry/traces"
)

type Review interface {
	GetHotelRatings(context.Context, []uuid.UUID) (map[uuid.UUID]dto.Rating, error)
}

// Service applies the same degradation policy to GetHotel and ListHotels.
type Service struct {
	review Review
	// Читаем текущий локальный snapshot, а не фиксируем флаг при создании сервиса.
	fallback func(context.Context) bool
}

func New(review Review, fallback func(context.Context) bool) *Service {
	return &Service{
		review:   review,
		fallback: fallback,
	}
}

func (s *Service) GetHotelRatings(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]dto.Rating, error) {
	ctx, span := traces.Start(ctx, "service.hotel.ratings")
	defer span.End()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values, err := s.review.GetHotelRatings(ctx, ids)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err == nil {
		return values, nil
	}
	if !s.fallback(ctx) || (!app.IsCode(err, app.Unavailable) && !app.IsCode(err, app.DeadlineExceeded)) {
		return nil, err
	}

	// Missing dependency data is not a zero rating or an empty review history.
	values = make(map[uuid.UUID]dto.Rating, len(ids))
	for _, id := range ids {
		values[id] = dto.Rating{Degraded: true}
	}
	span.AddEvent("fallback")
	slog.DebugContext(ctx, "ratings fallback", "hotels", len(ids))
	return values, nil
}
