package add

import (
	"context"
	"time"

	"github.com/gofrs/uuid"

	entity "review/internal/domain/entity/rating"
	"review/internal/events/rating_changed"
	"review/internal/pkg/event"
	"review/internal/pkg/transaction"
)

type Storage interface {
	Get(context.Context, []uuid.UUID) ([]*entity.Rating, error)
	Save(context.Context, *entity.Rating) error
}

type UseCase struct {
	storage Storage
	flusher event.Flusher
}

func New(storage Storage, flusher event.Flusher) *UseCase {
	return &UseCase{
		storage: storage,
		flusher: flusher,
	}
}

func (s *UseCase) Execute(ctx context.Context, input Input) (*entity.Rating, error) {
	ratings, err := s.storage.Get(ctx, []uuid.UUID{input.HotelID})
	if err != nil {
		return nil, err
	}

	rating := entity.New(input.HotelID)
	if len(ratings) != 0 {
		rating = ratings[0]
	}

	if err = rating.Add(input.Stars, time.Now().UTC()); err != nil {
		return nil, err
	}

	err = transaction.Exec(ctx, func(txCtx context.Context) error {
		if err := s.storage.Save(txCtx, rating); err != nil {
			return err
		}
		buffer := event.New(s.flusher)
		buffer.Add(rating_changed.New(rating))
		return buffer.Flush(txCtx)
	})
	if err != nil {
		return nil, err
	}
	return rating, nil
}
