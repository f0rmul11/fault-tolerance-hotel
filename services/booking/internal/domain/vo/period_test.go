package vo

import (
	"testing"
	"time"
)

func TestPeriod(t *testing.T) {
	cases := []struct {
		name  string
		start string
		end   string
		valid bool
	}{
		{
			name:  "12 hours",
			start: "2026-10-12T09:30:00+03:00",
			end:   "2026-10-12T21:30:00+03:00",
			valid: true,
		},
		{
			name:  "18 hours",
			start: "2026-10-12T09:30:00Z",
			end:   "2026-10-13T03:30:00Z",
			valid: true,
		},
		{
			name:  "duration policy belongs to room",
			valid: true,
			start: "2026-10-12T09:30:00Z",
			end:   "2026-10-12T21:29:59Z",
		},
		{
			name:  "equal",
			start: "2026-10-12T09:30:00Z",
			end:   "2026-10-12T09:30:00Z",
		},
		{
			name:  "backwards",
			start: "2026-10-13T09:30:00Z",
			end:   "2026-10-12T09:30:00Z",
		},
		{
			name:  "timezone missing",
			start: "2026-10-12T09:30:00",
			end:   "2026-10-13T09:30:00Z",
		},
		{
			name:  "precision loss",
			start: "2026-10-12T09:30:00.0000001Z",
			end:   "2026-10-13T09:30:00Z",
		},
		{
			name:  "no seed horizon",
			start: "2030-01-01T13:45:00Z",
			end:   "2030-03-01T14:15:00Z",
			valid: true,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParsePeriod(test.start, test.end)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
}

func TestPeriodAndLimits(t *testing.T) {
	p, _ := ParsePeriod("2026-10-12T00:00:00Z", "2026-10-12T12:00:00Z")
	same, _ := ParsePeriod("2026-10-12T03:00:00+03:00", "2026-10-12T15:00:00+03:00")
	if !p.Equal(same) || p.Duration() != 12*time.Hour {
		t.Fatal("time zone changed the period")
	}
	for _, tc := range []struct {
		name   string
		limits Limits
		want   bool
	}{
		{"minimum", Limits{Min: 12 * time.Hour}, true},
		{"maximum", Limits{
			Min: time.Hour,
			Max: 12 * time.Hour,
		}, true},
		{"too short", Limits{Min: 24 * time.Hour}, false},
		{"too long", Limits{
			Min: time.Hour,
			Max: 6 * time.Hour,
		}, false},
		{"invalid limits", Limits{
			Min: 24 * time.Hour,
			Max: 12 * time.Hour,
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.limits.Allows(p) != tc.want {
				t.Fatal("wrong duration policy")
			}
		})
	}
}
