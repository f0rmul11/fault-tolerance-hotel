package get

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/gofrs/uuid"

	entity "hotels/internal/domain/entity/hotel"
	dto "hotels/internal/dto/hotel"
)

type storage struct {
	hotel *entity.Hotel
	err   error
}

func (s *storage) Get(context.Context, uuid.UUID) (*entity.Hotel, error) {
	return s.hotel, s.err
}

type review struct {
	ratings map[uuid.UUID]dto.Rating
	ids     []uuid.UUID
	calls   int
	err     error
}

func (r *review) GetHotelRatings(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]dto.Rating, error) {
	r.calls++
	r.ids = ids
	return r.ratings, r.err
}

func TestGetHotelSetsItsRating(t *testing.T) {
	id := uuid.FromStringOrNil("cdd4685e-c610-4e7e-95ee-c51fb6807400")
	otherID := uuid.FromStringOrNil("abca2095-b122-4323-bb20-459d2cc86818")
	for _, tt := range []struct {
		name   string
		rating dto.Rating
	}{
		{
			name: "rated",
			rating: dto.Rating{
				Value:       4.5,
				ReviewCount: 10,
			},
		},
		{
			name: "real zero",
			rating: dto.Rating{
				Value:       0,
				ReviewCount: 2,
			},
		},
		{name: "no reviews"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			hotel := entity.With(id)
			ratings := map[uuid.UUID]dto.Rating{otherID: {
				Value:       5,
				ReviewCount: 3,
			}}
			if tt.rating.ReviewCount != 0 {
				ratings[id] = tt.rating
			}
			client := &review{ratings: ratings}

			result, err := New(&storage{hotel: hotel}, client).Execute(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if result != hotel || hotel.Rating() != tt.rating {
				t.Fatalf("expected original hotel with rating %+v, got %+v", tt.rating, result)
			}
			if client.calls != 1 || !slices.Equal(client.ids, []uuid.UUID{id}) {
				t.Fatalf("unexpected rating request: %d %v", client.calls, client.ids)
			}
		})
	}
}

func TestGetHotelErrors(t *testing.T) {
	id := uuid.FromStringOrNil("cdd4685e-c610-4e7e-95ee-c51fb6807400")
	failure := errors.New("unavailable")
	for _, tt := range []struct {
		name        string
		id          uuid.UUID
		storageErr  error
		reviewErr   error
		reviewCalls int
	}{
		{name: "invalid id"},
		{
			name:       "storage failure",
			id:         id,
			storageErr: failure,
		},
		{
			name:        "review failure",
			id:          id,
			reviewErr:   failure,
			reviewCalls: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			hotel := entity.With(id)
			client := &review{err: tt.reviewErr}
			result, err := New(&storage{
				hotel: hotel,
				err:   tt.storageErr,
			}, client).Execute(context.Background(), tt.id)
			if err == nil || result != nil || client.calls != tt.reviewCalls {
				t.Fatalf("result=%v error=%v calls=%d", result, err, client.calls)
			}
			if hotel.Rating() != (dto.Rating{}) {
				t.Fatal("failed read must not enrich hotel")
			}
		})
	}
}
