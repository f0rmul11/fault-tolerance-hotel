package refresh

import (
	"context"

	"github.com/gofrs/uuid"

	app "hotels/internal/pkg/hotelserror"
	"hotels/internal/pkg/telemetry/traces"
)

type Adapter interface {
	Refresh(context.Context, uuid.UUID) error
}

type UseCase struct {
	adapter Adapter
}

func New(adapter Adapter) *UseCase {
	return &UseCase{
		adapter: adapter,
	}
}

// Execute обновляет снимок отеля после изменения рейтинга в Review.
func (s *UseCase) Execute(ctx context.Context, id uuid.UUID) error {
	ctx, span := traces.Start(ctx, "usecase.hotel.refresh")
	defer span.End()

	if id == uuid.Nil {
		return app.NewInvalidArgumentErr("Некорректный идентификатор отеля в событии")
	}

	return s.adapter.Refresh(ctx, id)
}
