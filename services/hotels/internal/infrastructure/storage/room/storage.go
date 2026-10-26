package room

import (
	"context"
	"errors"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	entity "hotels/internal/domain/entity/room"
	"hotels/internal/infrastructure/storage/room/dao"
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

func (s *Storage) Get(ctx context.Context, id int64) (*entity.Room, error) {
	ctx, span := traces.Start(ctx, "storage.room.Get")
	defer span.End()

	const query = `
		select *
		from public.rooms
		where id = $1
	`
	var row dao.Room
	if err := pgxscan.Get(ctx, s.db.Pool(ctx), &row, query, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, app.NewNotFoundErr("Номер не найден", err)
		}

		return nil, err
	}

	return row.ConvertTo(), nil
}

func (s *Storage) List(ctx context.Context, hotelID uuid.UUID) ([]*entity.Room, error) {
	ctx, span := traces.Start(ctx, "storage.room.List")
	defer span.End()

	const query = `
		select *
		from public.rooms
		where hotel_id = $1
		order by number, id
	`

	var rows []dao.Room
	if err := pgxscan.Select(ctx, s.db.Pool(ctx), &rows,
		query, hotelID,
	); err != nil {
		return nil, err
	}
	rooms := make([]*entity.Room, 0, len(rows))
	for _, row := range rows {
		rooms = append(rooms, row.ConvertTo())
	}
	return rooms, nil
}
