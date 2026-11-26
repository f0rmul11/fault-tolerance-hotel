package get

import (
	"context"
	"errors"
	"testing"

	"github.com/gofrs/uuid"

	entity "hotels/internal/domain/entity/hotel"
	dto "hotels/internal/dto/hotel"
)

type adapterFake struct {
	hotel      *entity.Hotel
	err        error
	calls      int
	minVersion int64
}

func (a *adapterFake) Get(_ context.Context, _ uuid.UUID, minVersion int64) (*entity.Hotel, error) {
	a.calls++
	a.minVersion = minVersion
	return a.hotel, a.err
}

func TestGetHotel(t *testing.T) {
	id := uuid.Must(uuid.NewV4())
	hotel := entity.With(id)
	hotel.SetRating(dto.Rating{
		Value:       4.5,
		ReviewCount: 10,
		Version:     42,
	})
	adapter := &adapterFake{hotel: hotel}

	result, err := New(adapter).Execute(t.Context(), Input{HotelID: id, MinRatingVersion: 42})
	if err != nil || result != hotel || result.Rating() != hotel.Rating() || adapter.calls != 1 || adapter.minVersion != 42 {
		t.Fatalf("result=%v error=%v calls=%d", result, err, adapter.calls)
	}
}

func TestGetHotelErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		id         uuid.UUID
		err        error
		calls      int
		minVersion int64
	}{
		{name: "invalid id"},
		{name: "negative minimum version", id: uuid.Must(uuid.NewV4()), minVersion: -1},
		{
			name:  "adapter failure",
			id:    uuid.Must(uuid.NewV4()),
			err:   errors.New("origin unavailable"),
			calls: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter := &adapterFake{err: test.err}
			result, err := New(adapter).Execute(t.Context(), Input{HotelID: test.id, MinRatingVersion: test.minVersion})
			if err == nil || result != nil || adapter.calls != test.calls {
				t.Fatalf("result=%v error=%v calls=%d", result, err, adapter.calls)
			}
		})
	}
}
