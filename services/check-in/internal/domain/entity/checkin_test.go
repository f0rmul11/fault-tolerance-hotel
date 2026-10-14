package entity

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"

	"check-in/internal/domain/vo"
	app "check-in/internal/pkg/checkinerror"
)

func TestArrivalEligibility(t *testing.T) {
	start := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	period, _ := vo.NewPeriod(start, start.Add(24*time.Hour))
	for _, tc := range []struct {
		name    string
		status  Status
		at      time.Time
		allowed bool
	}{
		{"at start", Expected, start, true}, {"during stay", Expected, start.Add(time.Hour), true},
		{"before stay", Expected, start.Add(-time.Second), false}, {"at end", Expected, period.End(), false},
		{"already checked in", CheckedIn, start, false}, {"pending", Arriving, start, false}, {"canceled", Canceled, start, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := With(uuid.Must(uuid.NewV4()), WithStatus(tc.status), WithPeriod(period), WithVersion(1))
			err := c.Arrive(uuid.Must(uuid.NewV4()), uuid.Must(uuid.NewV4()), tc.at)
			if !tc.allowed && !app.IsCode(err, app.FailedPrecondition) {
				t.Fatalf("expected business error: %v", err)
			}
			if (err == nil) != tc.allowed {
				t.Fatalf("err=%v", err)
			}
			if tc.allowed && (c.Status() != Arriving || c.Version() != 2 || c.OldVersion() != 1) {
				t.Fatal("wrong arrival transition")
			}
			if !tc.allowed && (c.Status() != tc.status || c.Version() != 1) {
				t.Fatal("rejection changed state")
			}
		})
	}
}

func TestCancellationEligibility(t *testing.T) {
	for _, tc := range []struct {
		status  Status
		allowed bool
	}{{Expected, true}, {CheckedIn, true}, {Arriving, false}, {Canceling, false}, {Canceled, false}} {
		t.Run(string(tc.status), func(t *testing.T) {
			c := With(uuid.Must(uuid.NewV4()), WithStatus(tc.status), WithVersion(3))
			err := c.Cancel(uuid.Must(uuid.NewV4()), uuid.Must(uuid.NewV4()), time.Now())
			if !tc.allowed && !app.IsCode(err, app.FailedPrecondition) {
				t.Fatalf("expected business error: %v", err)
			}
			if (err == nil) != tc.allowed {
				t.Fatalf("err=%v", err)
			}
			if tc.allowed && (c.Status() != Canceling || c.PreviousStatus() != tc.status || c.Version() != 4) {
				t.Fatal("wrong cancellation")
			}
		})
	}
}

func TestRepliesDoNotRegressCheckIn(t *testing.T) {
	for _, tc := range []struct {
		name           string
		status         Status
		previous       Status
		version        int
		requestVersion int
		reply          string
		want           Status
		changed        bool
	}{
		{"activation", Arriving, Expected, 2, 2, "arrival", CheckedIn, true},
		{"cancellation", Canceling, CheckedIn, 4, 4, "cancel", Canceled, true},
		{"old activation during cancellation", Canceling, CheckedIn, 4, 2, "arrival", Canceling, false},
		{"duplicate activation", CheckedIn, "", 3, 2, "arrival", CheckedIn, false},
		{"old refusal", Canceling, CheckedIn, 4, 2, "reject", Canceling, false},
		{"refused cancellation", Canceling, CheckedIn, 4, 4, "reject", CheckedIn, true},
		{"refused activation", Arriving, Expected, 2, 2, "reject", Expected, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := With(uuid.Must(uuid.NewV4()), WithStatus(tc.status), WithPreviousStatus(tc.previous), WithVersion(tc.version))
			var changed bool
			switch tc.reply {
			case "arrival":
				changed = c.ConfirmArrival(tc.requestVersion)
			case "cancel":
				changed = c.ConfirmCancellation(tc.requestVersion)
			case "reject":
				changed = c.Reject(tc.requestVersion, "Действие отклонено")
			}
			if changed != tc.changed || c.Status() != tc.want {
				t.Fatalf("changed=%v status=%s", changed, c.Status())
			}
			wantVersion := tc.version
			if tc.changed {
				wantVersion++
			}
			if c.Version() != wantVersion {
				t.Fatal("unexpected version")
			}
		})
	}
}
