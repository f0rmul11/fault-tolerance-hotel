package rating

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	app "review/internal/pkg/reviewerror"
)

func TestAddRating(t *testing.T) {
	id := uuid.Must(uuid.NewV4())
	at := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	for _, stars := range []int64{0, 1, 5, 6} {
		t.Run(time.Duration(stars).String(), func(t *testing.T) {
			rating := With(id, WithTotalStars(42), WithReviewCount(10), WithVersion(7))
			err := rating.Add(stars, at)
			if stars < 1 || stars > 5 {
				if !app.IsCode(err, app.InvalidArgument) || rating.TotalStars() != 42 || rating.ReviewCount() != 10 || rating.Version() != 7 || !rating.UpdatedAt().IsZero() {
					t.Fatalf("invalid rating changed state: %+v, %v", rating, err)
				}
				return
			}
			if err != nil || rating.TotalStars() != 42+stars || rating.ReviewCount() != 11 || rating.Version() != 8 || rating.OldVersion() != 7 || !rating.UpdatedAt().Equal(at) {
				t.Fatalf("unexpected state: %+v, %v", rating, err)
			}
		})
	}
	rating := New(id)
	if err := rating.Add(5, at); err != nil || rating.Version() != 1 || rating.OldVersion() != 0 || rating.Value() != 5 {
		t.Fatalf("first rating: %+v, %v", rating, err)
	}
}
