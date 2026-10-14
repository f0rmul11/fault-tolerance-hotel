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
	err error
}

func (s *storageFake) Create(context.Context, *entity.Booking, uuid.UUID) error {
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
		uuid.Must(uuid.NewV4()), vo.Limits{Min: 12 * time.Hour})
	if !errors.Is(err, conflict) || result != nil {
		t.Fatalf("conflict must reach caller without a booking: result=%v err=%v", result, err)
	}
	if len(flusher.events) != 0 {
		t.Fatalf("conflicting request published %d events", len(flusher.events))
	}
}
