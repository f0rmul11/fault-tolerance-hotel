//go:build integration

package add_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"review/internal/application/usecase/rating/add"
	entity "review/internal/domain/entity/rating"
	storage "review/internal/infrastructure/storage/rating"
	"review/internal/pkg/event"
	"review/internal/pkg/outbox"
	app "review/internal/pkg/reviewerror"
	"review/internal/pkg/transaction/wrapper"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type failingFlusher struct{}

func (failingFlusher) Flush(context.Context, event.Events) error {
	return errors.New("outbox unavailable")
}

type snapshotBarrier struct {
	add.Storage
	ready   chan struct{}
	release chan struct{}
}

func (s snapshotBarrier) Get(ctx context.Context, ids []uuid.UUID) ([]*entity.Rating, error) {
	ratings, err := s.Storage.Get(ctx, ids)
	s.ready <- struct{}{}
	<-s.release
	return ratings, err
}

// Проверка настоящих SQL/CAS/rollback в отдельной мигрированной БД.
func TestRatingPersistence(t *testing.T) {
	dsn := os.Getenv("REVIEW_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set REVIEW_TEST_POSTGRES_DSN to an isolated migrated database")
	}
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := storage.New(wrapper.NewDatabase(pool))
	queue := outbox.NewOutbox(pool, outbox.Config{})
	usecase := add.New(store, queue)
	id := uuid.Must(uuid.NewV4())
	defer func() {
		_, _ = pool.Exec(ctx, "delete from public.outbox where entity_id=$1", id.String())
		_, _ = pool.Exec(ctx, "delete from public.ratings where hotel_id=$1", id)
	}()
	result, err := usecase.Execute(ctx, add.Input{HotelID: id, Stars: 5})
	if err != nil || result.Version() != 1 || result.Value() != 5 {
		t.Fatalf("first rating: %v, %v", result, err)
	}
	result, err = usecase.Execute(ctx, add.Input{HotelID: id, Stars: 3})
	if err != nil || result.Version() != 2 || result.ReviewCount() != 2 || result.Value() != 4 {
		t.Fatalf("update: %v, %v", result, err)
	}
	_, err = add.New(store, failingFlusher{}).Execute(ctx, add.Input{HotelID: id, Stars: 1})
	if err == nil {
		t.Fatal("failed outbox accepted")
	}
	ratings, err := store.Get(ctx, []uuid.UUID{id})
	if err != nil || len(ratings) != 1 || ratings[0].Version() != 2 || ratings[0].TotalStars() != 8 {
		t.Fatalf("rollback: %v, %v", ratings, err)
	}
	var messages int
	if err := pool.QueryRow(ctx, "select count(*) from public.outbox where entity_id=$1", id.String()).Scan(&messages); err != nil || messages != 2 {
		t.Fatalf("outbox count=%d, %v", messages, err)
	}

	// Оба запроса получают одну версию до начала записи; один должен проиграть CAS.
	gate := snapshotBarrier{Storage: store, ready: make(chan struct{}, 2), release: make(chan struct{})}
	errorsCh := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := add.New(gate, queue).Execute(ctx, add.Input{HotelID: id, Stars: 5})
			errorsCh <- err
		}()
	}
	<-gate.ready
	<-gate.ready
	close(gate.release)
	group.Wait()
	close(errorsCh)
	successes, conflicts := 0, 0
	for err := range errorsCh {
		if err == nil {
			successes++
		} else if app.IsCode(err, app.Conflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", successes, conflicts)
	}
	ratings, err = store.Get(ctx, []uuid.UUID{id})
	if err != nil || ratings[0].Version() != 3 || ratings[0].ReviewCount() != 3 || ratings[0].TotalStars() != 13 {
		t.Fatalf("concurrent state: %v, %v", ratings, err)
	}
	if err := pool.QueryRow(ctx, "select count(*) from public.outbox where entity_id=$1", id.String()).Scan(&messages); err != nil || messages != 3 {
		t.Fatalf("outbox after conflict=%d, %v", messages, err)
	}
}
