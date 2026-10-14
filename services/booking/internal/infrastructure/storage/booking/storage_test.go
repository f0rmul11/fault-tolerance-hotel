package booking

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"booking/internal/domain/entity"
	"booking/internal/domain/vo"
	app "booking/internal/pkg/bookingerror"
	"booking/internal/pkg/transaction/wrapper"
)

type databaseFake struct{ wrapper.Executor }

func (d databaseFake) Pool(context.Context) wrapper.Executor {
	return d.Executor
}

type executorFake struct {
	wrapper.Executor
	tag  string
	err  error
	args []any
}

func (e *executorFake) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	e.args = args
	return pgconn.NewCommandTag(e.tag), e.err
}

func TestCreate(t *testing.T) {
	at := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	period, _ := vo.NewPeriod(at, at.Add(12*time.Hour))
	b := entity.With(uuid.Must(uuid.NewV4()),
		entity.WithPeriod(period),
		entity.WithRoomID(1),
		entity.WithRoomNumber("101"),
		entity.WithRoomType("Standard"))
	for _, tc := range []struct {
		name string
		tag  string
		err  error
		code app.Code
		raw  bool
	}{
		{
			name: "one booking",
			tag:  "INSERT 0 1",
		},
		{
			name: "duplicate key",
			tag:  "INSERT 0 0",
			code: app.Conflict,
		},
		{
			name: "unexpected count",
			tag:  "INSERT 0 2",
			code: app.Internal,
		},
		{
			name: "room overlap",
			err: &pgconn.PgError{
				Code:           "23P01",
				ConstraintName: "bookings_room_period_excl",
			},
			code: app.FailedPrecondition,
		},
		{
			name: "different constraint",
			err: &pgconn.PgError{
				Code:           "23P01",
				ConstraintName: "other",
			},
			raw: true,
		},
		{
			name: "concurrent insert deadlock",
			err:  &pgconn.PgError{Code: "40P01"},
			code: app.Conflict,
		},
		{
			name: "database failure",
			err:  errors.New("connection lost"),
			raw:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			executor := &executorFake{
				tag: tc.tag,
				err: tc.err,
			}
			err := New(databaseFake{executor}).Create(context.Background(), b, uuid.Must(uuid.NewV4()))
			switch {
			case tc.raw:
				if err != tc.err {
					t.Fatalf("lost cause: %v", err)
				}
			case tc.code != 0:
				if !app.IsCode(err, tc.code) {
					t.Fatalf("code=%v err=%v", tc.code, err)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
			}
			if len(executor.args) != 13 || executor.args[3] != period.Start() || executor.args[4] != period.End() || executor.args[2] != int64(1) || executor.args[11] != "101" || executor.args[12] != "Standard" {
				t.Fatal("wrong booking period")
			}
		})
	}
}
