package hotel

import (
	"context"
	"errors"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	entity "hotels/internal/domain/entity/hotel"
	"hotels/internal/infrastructure/storage/hotel/dao"
	app "hotels/internal/pkg/hotelserror"
	"hotels/internal/pkg/telemetry/traces"
	"hotels/internal/pkg/transaction/wrapper"
)

type Storage struct {
	db wrapper.Database
}

func New(db wrapper.Database) *Storage {
	return &Storage{db: db}
}

func (s *Storage) Get(ctx context.Context, id uuid.UUID) (*entity.Hotel, error) {
	ctx, span := traces.Start(ctx, "storage.hotel.Get")
	defer span.End()

	const query = `
		select *
		from public.hotels
		where id = $1
	`
	var row dao.Hotel
	if err := pgxscan.Get(ctx, s.db.Pool(ctx), &row, query, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, app.NewNotFoundErr("Отель не найден", err)
		}

		return nil, err
	}

	return row.ConvertTo(), nil
}

func (s *Storage) List(ctx context.Context, limit, offset int) ([]*entity.Hotel, error) {
	ctx, span := traces.Start(ctx, "storage.hotel.List")
	defer span.End()

	const query = `
		select *
		from public.hotels
		order by id
		limit $1
		offset $2
	`

	var rows []dao.Hotel
	if err := pgxscan.Select(ctx, s.db.Pool(ctx), &rows,
		query, limit, offset,
	); err != nil {
		return nil, err
	}
	result := make([]*entity.Hotel, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.ConvertTo())
	}
	return result, nil
}
