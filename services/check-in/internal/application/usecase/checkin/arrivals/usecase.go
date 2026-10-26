package arrivals

import (
	"context"
	"time"

	"check-in/internal/domain/entity"
	"check-in/internal/domain/vo"
	"check-in/internal/pkg/telemetry/traces"
)

type Storage interface {
	ListArrivals(context.Context, vo.Period) ([]*entity.CheckIn, error)
}

type UseCase struct {
	storage  Storage
	location *time.Location
}

func New(storage Storage, location *time.Location) *UseCase {
	return &UseCase{
		storage:  storage,
		location: location,
	}
}

func (s *UseCase) Execute(ctx context.Context, input Input) ([]*entity.CheckIn, error) {
	ctx, span := traces.Start(ctx, "usecase.checkin.arrivals")
	defer span.End()

	period, ok := input.Period.Get()
	if !ok {
		now := time.Now().In(s.location)
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.location)
		period, _ = vo.NewPeriod(start, start.AddDate(0, 0, 1))
	}
	if err := period.Validate(); err != nil {
		return nil, err
	}
	return s.storage.ListArrivals(ctx, period)
}
