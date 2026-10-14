package rating

import (
	"testing"

	"github.com/gofrs/uuid"
)

func TestRatingValue(t *testing.T) {
	for _, tt := range []struct {
		name       string
		sum, count int64
		want       float64
	}{
		{name: "no reviews"},
		{
			name:  "real zero",
			count: 2,
		},
		{
			name:  "maximum",
			sum:   10,
			count: 2,
			want:  5,
		},
		{
			name:  "fractional average",
			sum:   45,
			count: 10,
			want:  4.5,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := With(uuid.Nil, WithTotalStars(tt.sum), WithReviewCount(tt.count))
			if r.Value() != tt.want {
				t.Fatalf("rating = %v, want %v", r.Value(), tt.want)
			}
		})
	}
}
