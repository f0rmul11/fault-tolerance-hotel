package review

import (
	"context"

	"github.com/gofrs/uuid"
	"github.com/samber/lo"

	entity "review/internal/domain/entity/rating"
	pb "review/internal/pkg/pb/review/review/v1"
	"review/internal/pkg/telemetry/traces"
)

func (s *Service) GetHotelRatings(ctx context.Context, req *pb.GetHotelRatingsRequest) (*pb.GetHotelRatingsResponse, error) {
	ctx, span := traces.Start(ctx, "handler.review.GetHotelRatings")
	defer span.End()

	ratings, err := s.usecases.GetHotelRatings.Execute(ctx, lo.Map(req.GetHotelIds(), func(id string, _ int) uuid.UUID {
		return uuid.FromStringOrNil(id)
	}))
	if err != nil {
		return nil, err
	}
	return &pb.GetHotelRatingsResponse{Ratings: lo.Map(ratings, func(r *entity.Rating, _ int) *pb.HotelRating {
		return &pb.HotelRating{
			HotelId:     r.ID().String(),
			Rating:      r.Value(),
			ReviewCount: r.ReviewCount(),
		}
	})}, nil
}
