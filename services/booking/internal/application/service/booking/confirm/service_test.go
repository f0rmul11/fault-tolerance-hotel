package confirm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gofrs/uuid"

	"booking/internal/domain/entity"
	"booking/internal/domain/vo"

	app "booking/internal/pkg/bookingerror"
	"booking/internal/pkg/event"
)

type storageFake struct {
	err      error
	version  int64
	stateErr error
	created  int
	states   int
}

func (s *storageFake) Create(context.Context, *entity.Booking, uuid.UUID) error {
	s.created++
	return s.err
}

type flusherFake struct {
	events event.Events
}

func (f *flusherFake) Flush(_ context.Context, events event.Events) error {
	f.events = append(f.events, events...)
	return nil
}

func TestConcurrentInsertConflictDoesNotPublishEvent(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	period, err := vo.NewPeriod(at, at.Add(12*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	booking, err := entity.NewBooking(uuid.Must(uuid.NewV4()), uuid.Must(uuid.NewV4()),
		1, uuid.Must(uuid.NewV4()), period, at)
	if err != nil {
		t.Fatal(err)
	}
	conflict := app.NewConflictErr("Ключ идемпотентности уже использован")
	flusher := &flusherFake{}
	result, err := New(&storageFake{err: conflict}, flusher).Confirm(t.Context(), booking,
		uuid.Must(uuid.NewV4()), vo.Limits{Min: 12 * time.Hour}, 0)
	if !errors.Is(err, conflict) || result != nil {
		t.Fatalf("conflict must reach caller without a booking: result=%v err=%v", result, err)
	}
	if len(flusher.events) != 0 {
		t.Fatalf("conflicting request published %d events", len(flusher.events))
	}
}

func (s *storageFake) GetBookingsVersion(_ context.Context, guestID uuid.UUID) (vo.Version, error) {
	return vo.Version{Old: s.version}, nil
}

func (s *storageFake) UpdateBookingsVersion(context.Context, uuid.UUID, vo.Version) error {
	s.states++
	return s.stateErr
}

func TestVersionConflictStopsCreation(t *testing.T) {
	at := time.Now().UTC()
	period, _ := vo.NewPeriod(at, at.Add(24*time.Hour))
	booking, _ := entity.NewBooking(uuid.Must(uuid.NewV4()), uuid.Must(uuid.NewV4()), 1, uuid.Must(uuid.NewV4()), period, at)
	for _, tc := range []struct {
		name       string
		expected   int64
		concurrent bool
	}{
		{name: "stale snapshot", expected: 6},
		{name: "concurrent change", expected: 7, concurrent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			storage := &storageFake{version: 7}
			if tc.concurrent {
				storage.stateErr = app.NewConflictErr("Список изменился")
			}
			flusher := &flusherFake{}
			result, err := New(storage, flusher).Confirm(t.Context(), booking, uuid.Must(uuid.NewV4()), vo.Limits{Min: time.Hour}, tc.expected)
			if result != nil || !app.IsCode(err, app.Conflict) || storage.created != 0 || len(flusher.events) != 0 {
				t.Fatalf("conflict created effects: result=%v err=%v creates=%d events=%d", result, err, storage.created, len(flusher.events))
			}
		})
	}
}
