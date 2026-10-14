package enrich_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/gofrs/uuid"
	"google.golang.org/grpc"

	"hotels/internal/application/service/hotel/enrich"
	entity "hotels/internal/domain/entity/hotel"
	dto "hotels/internal/dto/hotel"
	"hotels/internal/infrastructure/gateway/review"
	pb "hotels/internal/pkg/pb/external/review/review/v1"
)

type reviewClient struct {
	pb.ReviewServiceClient
	calls   int
	ids     []string
	ratings []*pb.HotelRating
	err     error
}

func (c *reviewClient) GetHotelRatings(_ context.Context, r *pb.GetHotelRatingsRequest, _ ...grpc.CallOption) (*pb.GetHotelRatingsResponse, error) {
	c.calls++
	c.ids = r.GetHotelIds()
	if c.err != nil {
		return nil, c.err
	}
	return &pb.GetHotelRatingsResponse{Ratings: c.ratings}, nil
}

func TestHotelsUseOneBatchAndMatchIDs(t *testing.T) {
	first := entity.With(uuid.FromStringOrNil("cdd4685e-c610-4e7e-95ee-c51fb6807400"))
	second := entity.With(uuid.FromStringOrNil("abca2095-b122-4323-bb20-459d2cc86818"))
	unrated := entity.With(uuid.FromStringOrNil("215227d1-7d60-4845-b751-f2e6040a2129"))
	hotels := []*entity.Hotel{first, second, unrated}
	client := &reviewClient{ratings: []*pb.HotelRating{
		{
			HotelId:     second.ID().String(),
			Rating:      0,
			ReviewCount: 2,
		},
		{
			HotelId:     first.ID().String(),
			Rating:      4.5,
			ReviewCount: 10,
		},
	}}

	err := enrich.New(review.NewClient(client)).Hotels(context.Background(), hotels)
	if err != nil {
		t.Fatal(err)
	}
	expectedIDs := []string{first.ID().String(), second.ID().String(), unrated.ID().String()}
	if client.calls != 1 || !slices.Equal(client.ids, expectedIDs) {
		t.Fatalf("expected one full batch, got %d calls %v", client.calls, client.ids)
	}
	for _, tt := range []struct {
		hotel  *entity.Hotel
		rating dto.Rating
	}{
		{
			hotel: first,
			rating: dto.Rating{
				Value:       4.5,
				ReviewCount: 10,
			},
		},
		{
			hotel: second,
			rating: dto.Rating{
				Value:       0,
				ReviewCount: 2,
			},
		},
		{hotel: unrated},
	} {
		if tt.hotel.Rating() != tt.rating {
			t.Errorf("hotel %s: got %+v, want %+v", tt.hotel.ID(), tt.hotel.Rating(), tt.rating)
		}
	}
}

func TestEmptyHotelsSkipReview(t *testing.T) {
	client := &reviewClient{}
	err := enrich.New(review.NewClient(client)).Hotels(context.Background(), nil)
	if err != nil || client.calls != 0 {
		t.Fatalf("empty list must skip review: %v", err)
	}
}

func TestReviewFailureDoesNotChangeHotels(t *testing.T) {
	client := &reviewClient{err: errors.New("offline")}
	hotel := entity.With(uuid.Nil)
	rating := dto.Rating{
		Value:       4.5,
		ReviewCount: 10,
	}
	hotel.SetRating(rating)

	err := enrich.New(review.NewClient(client)).Hotels(context.Background(), []*entity.Hotel{hotel})
	if err == nil || hotel.Rating() != rating {
		t.Fatalf("review failure must preserve hotel state: %v %+v", err, hotel.Rating())
	}
}
