package entity

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"

	"booking/internal/domain/vo"
	app "booking/internal/pkg/bookingerror"
)

func TestBookingTransitions(t *testing.T) {
	start := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	period, _ := vo.NewPeriod(start, start.Add(24*time.Hour))
	for _, tc := range []struct {
		name     string
		status   Status
		action   string
		at       time.Time
		want     Status
		changed  bool
		rejected bool
	}{
		{"activate", Confirmed, "activate", start, Active, true, false},
		{"duplicate activate", Active, "activate", start, Active, false, false},
		{"early arrival", Confirmed, "activate", start.Add(-time.Second), Confirmed, false, true},
		{"end is excluded", Confirmed, "activate", period.End(), Confirmed, false, true},
		{"canceled is terminal", Canceled, "activate", start, Canceled, false, true},
		{"cancel confirmed", Confirmed, "cancel", start, Canceled, true, false},
		{"cancel active", Active, "cancel", start, Canceled, true, false},
		{"duplicate cancel", Canceled, "cancel", start, Canceled, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := With(uuid.Must(uuid.NewV4()), WithStatus(tc.status), WithPeriod(period), WithVersion(1))
			var changed bool
			var err error
			if tc.action == "activate" {
				changed, err = b.Activate(tc.at)
			} else {
				changed, err = b.Cancel(tc.at)
			}
			if changed != tc.changed || (err != nil) != tc.rejected || b.Status() != tc.want {
				t.Fatalf("changed=%v status=%s err=%v", changed, b.Status(), err)
			}
			if tc.rejected && !app.IsCode(err, app.FailedPrecondition) {
				t.Fatalf("expected business error: %v", err)
			}
			wantVersion := 1
			if changed {
				wantVersion = 2
			}
			if b.Version() != wantVersion {
				t.Fatal("unexpected version")
			}
		})
	}
}
