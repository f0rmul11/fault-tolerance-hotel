package checkin

import (
	"context"
	"errors"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"check-in/internal/domain/entity"
	"check-in/internal/domain/vo"
	checkindto "check-in/internal/dto/checkin"
	"check-in/internal/infrastructure/storage/checkin/dao"
	app "check-in/internal/pkg/checkinerror"
	"check-in/internal/pkg/transaction/wrapper"
)

type Storage struct{ db wrapper.Database }

func New(db wrapper.Database) *Storage {
	return &Storage{db: db}
}

func (s *Storage) Create(ctx context.Context, c *entity.CheckIn) error {
	_, err := s.db.Pool(ctx).Exec(ctx, `insert into public.checkins
    (id,booking_id,room_id,guest_id,starts_at,ends_at,status,previous_status,version,failure,room_number,room_type)
    values ($1,$2,$3,$4,$5,$6,$7,'',$8,'',$9,$10) on conflict (booking_id) do nothing`,
		c.ID(), c.BookingID(), c.RoomID(), c.GuestID(), c.Period().Start(), c.Period().End(), string(c.Status()), c.Version(), c.RoomNumber(), c.RoomType())
	return err
}

func (s *Storage) Get(ctx context.Context, bookingID uuid.UUID) (*entity.CheckIn, error) {
	var row dao.CheckIn
	if err := pgxscan.Get(ctx, s.db.Pool(ctx), &row, `select * from public.checkins where booking_id=$1`, bookingID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, app.NewNotFoundErr("Заселение не найдено", err)
		}
		return nil, err
	}
	return row.ConvertTo()
}

func (s *Storage) ListArrivals(ctx context.Context, p vo.Period) ([]*entity.CheckIn, error) {
	var rows []dao.CheckIn
	if err := pgxscan.Select(ctx, s.db.Pool(ctx), &rows, `select * from public.checkins
    where status in ('EXPECTED','ARRIVING') and starts_at >= $1 and starts_at < $2 order by starts_at,id`, p.Start(), p.End()); err != nil {
		return nil, err
	}
	result := make([]*entity.CheckIn, 0, len(rows))
	for _, row := range rows {
		c, err := row.ConvertTo()
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

// ClaimRequest reserves an HTTP idempotency key in the same transaction as the state and outbox.
func (s *Storage) ClaimRequest(ctx context.Context, r checkindto.Request) (bool, error) {
	tag, err := s.db.Pool(ctx).Exec(ctx, `insert into public.requests (actor_id,key,booking_id,action)
    values ($1,$2,$3,$4) on conflict (actor_id,key) do nothing`, r.ActorID, r.Key, r.BookingID, string(r.Action))
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 1 {
		return true, nil
	}
	var row dao.Request
	if err := pgxscan.Get(
		ctx,
		s.db.Pool(ctx),
		&row,
		`select * from public.requests where actor_id=$1 and key=$2`,
		r.ActorID,
		r.Key,
	); err != nil {
		return false, err
	}
	if !row.ConvertTo().Matches(r.BookingID, r.Action) {
		return false, app.NewConflictErr("Ключ идемпотентности использован для другого действия")
	}
	return false, nil
}

func (s *Storage) Save(ctx context.Context, objects ...*entity.CheckIn) error {
	if len(objects) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(objects))
	statuses := make([]string, 0, len(objects))
	previous := make([]string, 0, len(objects))
	versions := make([]int64, 0, len(objects))
	oldVersions := make([]int64, 0, len(objects))
	actors := make([]uuid.UUID, 0, len(objects))
	requests := make([]uuid.UUID, 0, len(objects))
	requestedAt := make([]time.Time, 0, len(objects))
	failures := make([]string, 0, len(objects))
	for _, c := range objects {
		ids = append(ids, c.ID())
		statuses = append(statuses, string(c.Status()))
		previous = append(previous, string(c.PreviousStatus()))
		versions = append(versions, int64(c.Version()))
		oldVersions = append(oldVersions, int64(c.OldVersion()))
		actors = append(actors, c.ActorID())
		requests = append(requests, c.RequestID())
		requestedAt = append(requestedAt, c.RequestedAt())
		failures = append(failures, c.Failure())
	}
	tag, err := s.db.Pool(ctx).Exec(ctx, `update public.checkins c
    set status=v.status,previous_status=v.previous_status,version=v.version,
        actor_id=v.actor_id,request_id=v.request_id,requested_at=v.requested_at,failure=v.failure
    from unnest($1::uuid[],$2::text[],$3::text[],$4::bigint[],$5::bigint[],
        $6::uuid[],$7::uuid[],$8::timestamptz[],$9::text[])
        as v(id,status,previous_status,version,old_version,actor_id,request_id,requested_at,failure)
    where c.id=v.id and c.version=v.old_version`, ids, statuses, previous, versions, oldVersions, actors, requests, requestedAt, failures)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != int64(len(objects)) {
		return app.NewConflictErr("Заселение изменилось одновременно; повторите запрос с тем же ключом")
	}
	return nil
}
