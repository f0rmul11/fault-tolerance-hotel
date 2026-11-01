package booking_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"booking/internal/application/service/booking/confirm"
	"booking/internal/application/usecase/booking/activate"
	"booking/internal/application/usecase/booking/cancel"
	"booking/internal/domain/entity"
	"booking/internal/domain/vo"

	checkindto "booking/internal/dto/checkin"
	storage "booking/internal/infrastructure/storage/booking"
	app "booking/internal/pkg/bookingerror"
	"booking/internal/pkg/outbox"
	"booking/internal/pkg/transaction"
	"booking/internal/pkg/transaction/wrapper"
)

// Run only against an explicitly supplied, migrated disposable database.
func TestBookingsVersionPostgres(t *testing.T) {
	dsn := os.Getenv("BOOKING_TEST_DSN")
	if dsn == "" {
		t.Skip("set BOOKING_TEST_DSN to a migrated disposable PostgreSQL")
	}
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := storage.New(wrapper.NewDatabase(pool))
	flusher := outbox.NewOutbox(pool, outbox.Config{})
	service := confirm.New(store, flusher)
	guest := uuid.Must(uuid.NewV4())
	at := time.Now().UTC().Truncate(time.Microsecond)
	period, _ := vo.NewPeriod(at, at.Add(24*time.Hour))
	room := at.UnixNano()
	newBooking := func(roomID int64) *entity.Booking {
		b, err := entity.NewBooking(uuid.Must(uuid.NewV4()), guest, roomID, uuid.Must(uuid.NewV4()), period, at)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	assertList := func(version int64, size int) entity.BookingList {
		t.Helper()
		list, err := store.List(ctx, guest)
		if err != nil || list.Version.Current() != version || len(list.Bookings) != size {
			t.Fatalf("list version/size mismatch: got=%+v err=%v want=%d/%d", list, err, version, size)
		}
		return list
	}
	assertList(0, 0)

	// Two distinct keys and rooms, same initial snapshot: exactly one can commit.
	bookings := []*entity.Booking{newBooking(room), newBooking(room + 1)}
	start := make(chan struct{})
	outcomes := make(chan error, 2)
	var wg sync.WaitGroup
	for _, b := range bookings {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := service.Confirm(ctx, b, uuid.Must(uuid.NewV4()), vo.Limits{Min: time.Hour}, 0)
			outcomes <- err
		}()
	}
	close(start)
	wg.Wait()
	close(outcomes)
	successes, conflicts := 0, 0
	for err := range outcomes {
		if err == nil {
			successes++
		} else if app.IsCode(err, app.Conflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent creates: success=%d conflict=%d", successes, conflicts)
	}
	list := assertList(1, 1)
	saved := list.Bookings[0]
	var events int
	if err := pool.QueryRow(ctx, "select count(*) from outbox where entity_id = any($1)", []string{bookings[0].ID().String(), bookings[1].ID().String()}).Scan(&events); err != nil || events != 1 {
		t.Fatalf("expected exactly one committed outbox event: %d %v", events, err)
	}

	// A room conflict must roll back the collection increment too.
	_, err = service.Confirm(ctx, newBooking(saved.RoomID()), uuid.Must(uuid.NewV4()), vo.Limits{Min: time.Hour}, 1)
	if !app.IsCode(err, app.FailedPrecondition) {
		t.Fatalf("expected overlap: %v", err)
	}
	assertList(1, 1)

	// Real status changes advance the collection, repeated events do not.
	request := checkindto.Request{
		CorrelationID:  uuid.Must(uuid.NewV4()),
		CheckInID:      uuid.Must(uuid.NewV4()),
		BookingID:      saved.ID(),
		CheckInVersion: 1,
		ActorID:        uuid.Must(uuid.NewV4()),
		RequestID:      uuid.Must(uuid.NewV4()),
		RequestedAt:    at.Add(time.Minute),
	}
	for range 2 {
		if err := activate.New(store, flusher).Execute(ctx, activate.Input{Request: request}); err != nil {
			t.Fatal(err)
		}
		assertList(2, 1)
	}
	for range 2 {
		if err := cancel.New(store, flusher).Execute(ctx, cancel.Input{Request: request}); err != nil {
			t.Fatal(err)
		}
		if got := assertList(3, 1).Bookings[0].Status(); got != entity.Canceled {
			t.Fatalf("canceled booking missing: %s", got)
		}
	}

	// Commit another booking between the two SELECTs: the reader must keep its old snapshot.
	snapshot := storage.New(&snapshotDatabase{
		Database: wrapper.NewDatabase(pool),
		betweenReads: func() {
			if _, err := service.Confirm(ctx, newBooking(room+2), uuid.Must(uuid.NewV4()), vo.Limits{Min: time.Hour}, 3); err != nil {
				t.Fatal(err)
			}
		},
	})
	before, err := snapshot.List(ctx, guest)
	if err != nil || before.Version.Current() != 3 || len(before.Bookings) != 1 {
		t.Fatalf("mixed snapshots: %+v %v", before, err)
	}
	assertList(4, 2)

	// The collection version survives an empty result; another guest is isolated.
	emptyGuest := uuid.Must(uuid.NewV4())
	version := vo.Version{}
	version.Inc()
	if err := transaction.Exec(ctx, func(ctx context.Context) error {
		return store.UpdateBookingsVersion(ctx, emptyGuest, version)
	}); err != nil {
		t.Fatal(err)
	}
	empty, err := store.List(ctx, emptyGuest)
	if err != nil || empty.Version.Current() != 1 || len(empty.Bookings) != 0 {
		t.Fatalf("empty list lost version or guest isolation: %+v %v", empty, err)
	}
}

type snapshotDatabase struct {
	wrapper.Database
	betweenReads func()
}

func (d *snapshotDatabase) Pool(ctx context.Context) wrapper.Executor {
	return &snapshotExecutor{
		Executor:     d.Database.Pool(ctx),
		betweenReads: d.betweenReads,
	}
}

type snapshotExecutor struct {
	wrapper.Executor
	betweenReads func()
}

func (e *snapshotExecutor) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	if strings.Contains(query, "from public.bookings\n") {
		e.betweenReads()
	}
	return e.Executor.Query(ctx, query, args...)
}
