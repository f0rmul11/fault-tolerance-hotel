package booking

import (
	"context"
	"errors"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"booking/internal/domain/entity"
	"booking/internal/domain/vo"
	"booking/internal/infrastructure/storage/booking/dao"
	app "booking/internal/pkg/bookingerror"
	"booking/internal/pkg/transaction/abstract"
)

func (s *Storage) GetBookingsVersion(ctx context.Context, guestID uuid.UUID) (vo.Version, error) {
	const query = `
		select
			guest_id,
			version
		from public.bookings_state
		where guest_id = $1
	`
	var row dao.BookingsState
	if err := pgxscan.Get(ctx, s.db.Pool(ctx), &row, query, guestID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return vo.Version{}, nil
		}
		return vo.Version{}, err
	}
	return row.ConvertTo(), nil
}

// UpdateBookingsVersion участвует в транзакции изменения брони и outbox.
func (s *Storage) UpdateBookingsVersion(ctx context.Context, guestID uuid.UUID, version vo.Version) error {
	const query = `
		insert into public.bookings_state (
			guest_id,
			version
		)
		values ($1, $2)
		on conflict (guest_id) do update
		set version = excluded.version
		where bookings_state.version = $3
	`
	tag, err := s.db.Pool(ctx).Exec(ctx, query, guestID, version.Current(), version.Previous())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return app.NewConflictErr("Список броней изменился. Обновите список перед новой операцией")
	}
	return nil
}

// List читает версию и строки в одном MVCC-снимке, в том числе для пустого списка.
func (s *Storage) List(ctx context.Context, guestID uuid.UUID) (result entity.BookingList, err error) {
	ctx, finish := abstract.ContextWithTx(ctx, &pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	defer func() {
		if p := recover(); p != nil {
			_ = finish(errors.New("паника при чтении списка броней"))
			panic(p)
		}
		err = finish(err)
	}()
	version, err := s.GetBookingsVersion(ctx, guestID)
	if err != nil {
		return result, err
	}
	const query = `
		select *
		from public.bookings
		where guest_id = $1
		order by created_at, id
	`
	var rows []dao.Booking
	if err := pgxscan.Select(ctx, s.db.Pool(ctx), &rows, query, guestID); err != nil {
		return result, err
	}
	result.Version = version
	result.Bookings = make([]*entity.Booking, 0, len(rows))
	for _, row := range rows {
		booking, err := row.ConvertTo()
		if err != nil {
			return entity.BookingList{}, err
		}
		result.Bookings = append(result.Bookings, booking)
	}
	return result, nil
}
