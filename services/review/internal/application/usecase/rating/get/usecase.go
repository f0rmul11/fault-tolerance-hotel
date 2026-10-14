package get

import (
	"context"

	"github.com/gofrs/uuid"

	entity "review/internal/domain/entity/rating"
	app "review/internal/pkg/reviewerror"
)

type Storage interface {
	Get(context.Context, []uuid.UUID) ([]*entity.Rating, error)
}

type UseCase struct {
	storage Storage
}

func New(storage Storage) *UseCase {
	return &UseCase{storage: storage}
}

func (s *UseCase) Execute(ctx context.Context, ids []uuid.UUID) ([]*entity.Rating, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	for _, id := range ids {
		if id == uuid.Nil {
			return nil, app.NewInvalidArgumentErr("Некорректный идентификатор отеля")
		}
	}
	return s.storage.Get(ctx, ids)
}
