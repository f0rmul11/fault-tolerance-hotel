package booking

import (
	"context"
	"errors"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"booking/internal/domain/entity"
	"booking/internal/domain/vo"
	"booking/internal/infrastructure/storage/booking/dao"
	app "booking/internal/pkg/bookingerror"
	"booking/internal/pkg/telemetry/traces"
	"booking/internal/pkg/transaction/wrapper"
)

type Storage struct {
	db wrapper.Database
}

func New(db wrapper.Database) *Storage {
	return &Storage{db: db}
}

func (s *Storage) Create(ctx context.Context, b *entity.Booking, key uuid.UUID) error {
	ctx, span := traces.Start(ctx, "storage.booking.Create")
	defer span.End()

	const query = `
		insert into public.bookings (
			id,
			guest_id,
			room_id,
			starts_at,
			ends_at,
			request_id,
			status,
			idempotency_key,
			version,
			created_at,
			updated_at,
			room_number,
			room_type
		)
		values (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13
		)
		on conflict (guest_id, idempotency_key) do nothing
	`
	tag, err := s.db.Pool(ctx).Exec(ctx, query,
		b.ID(),
		b.GuestID(),
		b.RoomID(),
		b.Period().Start(),
		b.Period().End(),
		b.RequestID(),
		string(b.Status()),
		key,
		b.Version(),
		b.CreatedAt(),
		b.UpdatedAt(),
		b.RoomNumber(),
		b.RoomType(),
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "23P01" && pgErr.ConstraintName == "bookings_room_period_excl" {
				return app.NewBusinessErr("Номер занят в выбранное время")
			}
			if pgErr.Code == "40P01" {
				return app.NewConflictErr("Конкурирующее бронирование: повторите запрос с тем же ключом идемпотентности")
			}
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return app.NewConflictErr("Ключ идемпотентности уже использован")
	}
	if tag.RowsAffected() != 1 {
		return app.NewInternalError("Не удалось сохранить одну бронь", nil)
	}
	return nil
}

func (s *Storage) Get(ctx context.Context, id uuid.UUID) (*entity.Booking, error) {
	ctx, span := traces.Start(ctx, "storage.booking.Get")
	defer span.End()

	const query = `
		select *
		from public.bookings
		where id = $1
	`
	var row dao.Booking
	if err := pgxscan.Get(ctx, s.db.Pool(ctx), &row, query, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, app.NewNotFoundErr("Бронь не найдена", err)
		}
		return nil, err
	}
	return row.ConvertTo()
}

// FindByKey возвращает nil, если эта операция ещё не была сохранена.
func (s *Storage) FindByKey(ctx context.Context, guestID, key uuid.UUID) (*entity.Booking, error) {
	ctx, span := traces.Start(ctx, "storage.booking.FindByKey")
	defer span.End()

	const query = `
		select *
		from public.bookings
		where guest_id = $1
			and idempotency_key = $2
	`
	var row dao.Booking
	if err := pgxscan.Get(ctx, s.db.Pool(ctx), &row, query, guestID, key); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.ConvertTo()
}

// FreeRooms — снимок доступности; запрет пересечений при записи обеспечивает EXCLUDE.
func (s *Storage) FreeRooms(ctx context.Context, roomIDs []int64, period vo.Period) ([]int64, error) {
	ctx, span := traces.Start(ctx, "storage.booking.FreeRooms")
	defer span.End()

	const query = `
		select candidate.room_id
		from unnest($1::bigint[]) as candidate(room_id)
		where not exists (
			select 1
			from public.bookings b
			where b.room_id = candidate.room_id
				and b.status is distinct from 'CANCELED'
				and tstzrange(b.starts_at, b.ends_at, '[)') && tstzrange($2, $3, '[)')
		)
		order by candidate.room_id
	`
	rooms := make([]int64, 0, len(roomIDs))
	if err := pgxscan.Select(ctx, s.db.Pool(ctx), &rooms, query, roomIDs, period.Start(), period.End()); err != nil {
		return nil, err
	}
	return rooms, nil
}

// Save применяет доменную версию через CAS, одним SQL для всего набора.
func (s *Storage) Save(ctx context.Context, objects ...*entity.Booking) error {
	ctx, span := traces.Start(ctx, "storage.booking.Save")
	defer span.End()

	const query = `
		update public.bookings b
		set
			status = v.status,
			version = v.version,
			updated_at = v.updated_at
		from unnest(
			$1::uuid[],
			$2::text[],
			$3::bigint[],
			$4::bigint[],
			$5::timestamptz[]
		) as v(id, status, version, old_version, updated_at)
		where b.id = v.id
			and b.version = v.old_version
	`

	if len(objects) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(objects))
	statuses := make([]string, 0, len(objects))
	versions := make([]int64, 0, len(objects))
	oldVersions := make([]int64, 0, len(objects))
	updated := make([]time.Time, 0, len(objects))
	for _, b := range objects {
		ids = append(ids, b.ID())
		statuses = append(statuses, string(b.Status()))
		versions = append(versions, int64(b.Version()))
		oldVersions = append(oldVersions, int64(b.OldVersion()))
		updated = append(updated, b.UpdatedAt())
	}
	tag, err := s.db.Pool(ctx).Exec(ctx, query, ids, statuses, versions, oldVersions, updated)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != int64(len(objects)) {
		return app.NewConflictErr("Бронь изменилась одновременно")
	}
	return nil
}
