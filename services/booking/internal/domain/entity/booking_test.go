package entity

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"

	"booking/internal/domain/vo"
	app "booking/internal/pkg/bookingerror"
)

func TestBookingAndReplay(t *testing.T) {
	at := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	first, _ := vo.NewPeriod(at, at.Add(12*time.Hour))
	last, _ := vo.NewPeriod(at.Add(24*time.Hour), at.Add(36*time.Hour))
	guest := uuid.Must(uuid.NewV4())
	room := int64(1)
	period := first
	booking, err := NewBooking(uuid.Must(uuid.NewV4()), guest, room, uuid.Must(uuid.NewV4()), period, at)
	if err != nil {
		t.Fatal(err)
	}
	if booking.Status() != Confirmed || booking.Version() != 1 || booking.Period().Validate() != nil {
		t.Fatal("new booking must be confirmed and have a valid period")
	}

	for _, test := range []struct {
		name     string
		guest    uuid.UUID
		room     int64
		period   vo.Period
		conflict bool
	}{
		{"same request", guest, room, first, false},
		{"changed room", guest, 2, first, true},
		{"changed period", guest, room, last, true},
		{"changed guest", uuid.Must(uuid.NewV4()), room, first, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := NewBooking(
				uuid.Must(uuid.NewV4()),
				test.guest,
				test.room,
				uuid.Must(uuid.NewV4()),
				test.period,
				at.Add(time.Hour),
			)
			if err != nil {
				t.Fatal(err)
			}
			result, err := booking.Replay(request)
			if test.conflict {
				if !app.IsCode(err, app.Conflict) || result != nil {
					t.Fatalf("expected conflict, got %v, %v", result, err)
				}
			} else if err != nil || result != booking {
				t.Fatalf("replay changed original booking: %v", err)
			}
		})
	}

}
