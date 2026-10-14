package arrivals

import (
	"context"
	"testing"
	"time"

	"github.com/samber/mo"

	"check-in/internal/domain/entity"
	"check-in/internal/domain/vo"
)

type storageFake struct{ period vo.Period }

func (s *storageFake) ListArrivals(_ context.Context, period vo.Period) ([]*entity.CheckIn, error) {
	s.period = period
	return nil, nil
}

func TestArrivalDateRange(t *testing.T) {
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 10, 13, 21, 30, 0, 0, time.UTC)
	custom, _ := vo.NewPeriod(at, at.Add(5*time.Hour))
	for _, tc := range []struct {
		name   string
		period mo.Option[vo.Period]
	}{
		{
			name:   "default Moscow today",
			period: mo.None[vo.Period](),
		},
		{
			name:   "explicit range",
			period: mo.Some(custom),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			storage := &storageFake{}
			before := time.Now()
			_, err := New(storage, location).Execute(context.Background(), Input{Period: tc.period})
			after := time.Now()
			if err != nil {
				t.Fatal(err)
			}

			if period, ok := tc.period.Get(); ok {
				if !storage.period.Equal(period) {
					t.Fatalf("explicit range changed: %v", storage.period)
				}
				return
			}

			start := storage.period.Start().In(location)
			end := storage.period.End().In(location)
			if start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 || start.Nanosecond() != 0 {
				t.Fatalf("day must start at Moscow midnight: %v", start)
			}
			if !end.Equal(start.AddDate(0, 0, 1)) {
				t.Fatalf("wrong end of day: %v", end)
			}
			// A call crossing midnight may select either day, but never an unrelated one.
			if start.After(after) || !end.After(before) {
				t.Fatalf("selected day does not cover the call: %v–%v", start, end)
			}
		})
	}
}
