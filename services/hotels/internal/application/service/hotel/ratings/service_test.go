package ratings

import (
	"context"
	"errors"
	"testing"

	"github.com/gofrs/uuid"

	dto "hotels/internal/dto/hotel"
	app "hotels/internal/pkg/hotelserror"
)

type reviewFunc func(context.Context, []uuid.UUID) (map[uuid.UUID]dto.Rating, error)

func (f reviewFunc) GetHotelRatings(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]dto.Rating, error) {
	return f(ctx, ids)
}

func TestFallbackPolicy(t *testing.T) {
	id := uuid.Must(uuid.NewV4())
	for _, test := range []struct {
		name     string
		enabled  bool
		code     app.Code
		degraded bool
	}{
		{name: "unavailable", enabled: true, code: app.Unavailable, degraded: true},
		{name: "dependency_timeout", enabled: true, code: app.DeadlineExceeded, degraded: true},
		{name: "disabled", code: app.Unavailable},
		{name: "internal_error", enabled: true, code: app.Internal},
		{name: "business_error", enabled: true, code: app.NotFound},
		{name: "cancellation", enabled: true, code: app.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			failure := app.New(app.WithCode(int(test.code)), app.WithMessage("Ошибка зависимости"))
			service := New(reviewFunc(func(context.Context, []uuid.UUID) (map[uuid.UUID]dto.Rating, error) {
				return nil, failure
			}), test.enabled)
			values, err := service.GetHotelRatings(t.Context(), []uuid.UUID{id})
			if test.degraded {
				if err != nil || !values[id].Degraded || values[id].ReviewCount != 0 {
					t.Fatalf("values=%v error=%v", values, err)
				}
			} else if err != failure || values != nil {
				t.Fatalf("error was hidden: values=%v error=%v", values, err)
			}
		})
	}
}

func TestParentCancellationAndRecovery(t *testing.T) {
	id := uuid.Must(uuid.NewV4())
	ctx, cancel := context.WithCancel(t.Context())
	service := New(reviewFunc(func(context.Context, []uuid.UUID) (map[uuid.UUID]dto.Rating, error) {
		cancel()
		return nil, app.New(app.WithCode(int(app.Unavailable)))
	}), true)
	if _, err := service.GetHotelRatings(ctx, []uuid.UUID{id}); !errors.Is(err, context.Canceled) {
		t.Fatalf("parent cancellation masked: %v", err)
	}
	service.review = reviewFunc(func(context.Context, []uuid.UUID) (map[uuid.UUID]dto.Rating, error) {
		return map[uuid.UUID]dto.Rating{id: {Value: 4.5, ReviewCount: 20}}, nil
	})
	values, err := service.GetHotelRatings(t.Context(), []uuid.UUID{id})
	if err != nil || values[id].Degraded || values[id].Value != 4.5 || values[id].ReviewCount != 20 {
		t.Fatalf("healthy values were not restored: %v %v", values, err)
	}
}
