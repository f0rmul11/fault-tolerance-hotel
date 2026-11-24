package review

import (
	"context"

	"github.com/gofrs/uuid"

	"review/internal/application/usecase/rating/add"
	pb "review/internal/pkg/pb/review/review/v1"
	"review/internal/pkg/telemetry/traces"
)

func (s *Service) AddHotelRating(ctx context.Context, req *pb.AddHotelRatingRequest) (*pb.AddHotelRatingResponse, error) {
	ctx, span := traces.Start(ctx, "handler.review.AddHotelRating")
	defer span.End()

	rating, err := s.usecases.AddHotelRating.Execute(ctx, add.Input{
		HotelID: uuid.FromStringOrNil(req.GetHotelId()),
		Stars:   int64(req.GetStars()),
	})
	if err != nil {
		return nil, err
	}
	return &pb.AddHotelRatingResponse{Rating: &pb.HotelRating{
		HotelId:       rating.ID().String(),
		Rating:        rating.Value(),
		ReviewCount:   rating.ReviewCount(),
		RatingVersion: rating.Version(),
	}}, nil
}
