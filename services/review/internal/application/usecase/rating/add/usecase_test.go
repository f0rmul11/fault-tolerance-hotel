package add

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/gofrs/uuid"
	entity "review/internal/domain/entity/rating"
	"review/internal/events"
	"review/internal/events/rating_changed"
	"review/internal/pkg/event"
	app "review/internal/pkg/reviewerror"
	"review/internal/pkg/transaction/abstract"
)

type txFake struct {
	committed  bool
	rolledBack bool
	commitErr  error
}

func (tx *txFake) Commit() error {
	tx.committed = true
	return tx.commitErr
}
func (tx *txFake) Rollback() error {
	tx.rolledBack = true
	return nil
}

type storageFake struct {
	current   *entity.Rating
	saved     *entity.Rating
	saveErr   error
	tx        txFake
	contextTx *abstract.Tx
}

func (s *storageFake) Get(context.Context, []uuid.UUID) ([]*entity.Rating, error) {
	if s.current == nil {
		return nil, nil
	}
	return []*entity.Rating{s.current}, nil
}
func (s *storageFake) Save(ctx context.Context, rating *entity.Rating) error {
	s.contextTx = abstract.TxFromContext(ctx)
	s.contextTx.WithTx(ctx, func(context.Context) abstract.Transaction { return &s.tx })
	s.saved = rating
	return s.saveErr
}

type flusherFake struct {
	storage *storageFake
	err     error
	batch   event.Events
}

func (f *flusherFake) Flush(ctx context.Context, batch event.Events) error {
	if abstract.TxFromContext(ctx) != f.storage.contextTx {
		return errors.New("outbox uses a different transaction")
	}
	f.batch = batch
	return f.err
}

func TestAddAndOutboxTransaction(t *testing.T) {
	for _, scenario := range []string{"first", "existing", "conflict", "outbox failure", "commit failure", "invalid stars"} {
		t.Run(scenario, func(t *testing.T) {
			id := uuid.Must(uuid.NewV4())
			input := Input{HotelID: id, Stars: 5}
			storage := &storageFake{}
			flusher := &flusherFake{storage: storage}
			failure := errors.New("dependency failed")
			if scenario == "existing" {
				storage.current = entity.With(id, entity.WithTotalStars(42), entity.WithReviewCount(10), entity.WithVersion(7))
			}
			switch scenario {
			case "conflict":
				storage.saveErr = app.NewConflictErr("Рейтинг изменился")
			case "outbox failure":
				flusher.err = failure
			case "commit failure":
				storage.tx.commitErr = failure
			case "invalid stars":
				input.Stars = 6
			}
			result, err := New(storage, flusher).Execute(t.Context(), input)
			switch scenario {
			case "invalid stars":
				if !app.IsCode(err, app.InvalidArgument) || storage.saved != nil || len(flusher.batch) != 0 {
					t.Fatalf("invalid input wrote data: %v", err)
				}
			case "conflict":
				if !app.IsCode(err, app.Conflict) || !storage.tx.rolledBack || len(flusher.batch) != 0 {
					t.Fatalf("conflict: %v", err)
				}
			case "outbox failure":
				if !errors.Is(err, failure) || !storage.tx.rolledBack || storage.tx.committed || result != nil {
					t.Fatalf("outbox failure: %v", err)
				}
			case "commit failure":
				if !errors.Is(err, failure) || result != nil {
					t.Fatalf("commit failure was hidden: %v", err)
				}
			default:
				if err != nil || !storage.tx.committed || storage.tx.rolledBack || len(flusher.batch) != 1 {
					t.Fatalf("transaction: %v", err)
				}
				var envelope events.Base[rating_changed.RatingChanged]
				if err := json.Unmarshal(flusher.batch[0].Body, &envelope); err != nil {
					t.Fatal(err)
				}
				wantVersion := int64(1)
				if scenario == "existing" {
					wantVersion = 8
				}
				if result.Version() != wantVersion || envelope.RatingVersion != wantVersion || envelope.HotelID != id.String() {
					t.Fatalf("snapshot mismatch: %+v", envelope)
				}
			}
		})
	}
}
