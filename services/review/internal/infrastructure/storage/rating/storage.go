package rating

import (
	"context"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/gofrs/uuid"

	entity "review/internal/domain/entity/rating"
	"review/internal/infrastructure/storage/rating/dao"
	"review/internal/pkg/telemetry/traces"
	"review/internal/pkg/transaction/wrapper"
)

type Storage struct {
	db wrapper.Database
}

func New(db wrapper.Database) *Storage {
	return &Storage{db: db}
}

func (s *Storage) Get(ctx context.Context, ids []uuid.UUID) ([]*entity.Rating, error) {
	ctx, span := traces.Start(ctx, "storage.rating.Get")
	defer span.End()

	const query = `
		select *
		from public.ratings
		where hotel_id = any($1::uuid[])
		order by hotel_id
	`

	if len(ids) == 0 {
		return nil, nil
	}
	var rows []dao.Rating
	if err := pgxscan.Select(ctx, s.db.Pool(ctx), &rows,
		query, ids,
	); err != nil {
		return nil, err
	}
	result := make([]*entity.Rating, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.ConvertTo())
	}
	return result, nil
}
