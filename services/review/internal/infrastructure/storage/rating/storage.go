package rating

import (
	"context"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/gofrs/uuid"

	entity "review/internal/domain/entity/rating"
	"review/internal/infrastructure/storage/rating/dao"
	app "review/internal/pkg/reviewerror"
	"review/internal/pkg/telemetry/traces"
	"review/internal/pkg/transaction/wrapper"
)

type Storage struct {
	db wrapper.Database
}

func New(db wrapper.Database) *Storage {
	return &Storage{db: db}
}

func (s *Storage) Save(ctx context.Context, rating *entity.Rating) error {
	const query = `
		insert into public.ratings (hotel_id, total_stars, review_count, version, updated_at)
		values ($1, $2, $3, $4, $5)
		on conflict (hotel_id) do update set
			total_stars = excluded.total_stars,
			review_count = excluded.review_count,
			version = excluded.version,
			updated_at = excluded.updated_at
		where ratings.version = $6
	`
	result, err := s.db.Pool(ctx).Exec(ctx, query,
		rating.ID(), rating.TotalStars(), rating.ReviewCount(),
		rating.Version(), rating.UpdatedAt(), rating.OldVersion(),
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return app.NewConflictErr("Рейтинг изменился. Повторите операцию с актуальными данными")
	}
	return nil
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
