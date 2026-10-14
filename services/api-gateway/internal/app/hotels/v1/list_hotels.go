package hotels

import (
	pb "api-gateway/internal/pkg/pb/api-gateway/hotels/v1"
	external "api-gateway/internal/pkg/pb/external/hotels/hotels/v1"
	"context"

	"github.com/samber/lo"
)

func (i *Implementation) ListHotels(ctx context.Context, req *pb.ListHotelsRequest) (*pb.ListHotelsResponse, error) {
	response, err := i.external.ListHotels(ctx, &external.ListHotelsRequest{
		Limit:  req.Limit,
		Offset: req.GetOffset(),
	})
	if err != nil {
		return nil, err
	}
	return &pb.ListHotelsResponse{Hotels: lo.Map(response.GetHotels(), func(r *external.Hotel, _ int) *pb.Hotel {
		return &pb.Hotel{
			Id:          r.GetId(),
			Name:        r.GetName(),
			Description: r.GetDescription(),
			Stars:       r.GetStars(),
			Country:     r.GetCountry(),
			City:        r.GetCity(),
			Address:     r.GetAddress(),
			Latitude:    r.GetLatitude(),
			Longitude:   r.GetLongitude(),
			ReviewCount: r.GetReviewCount(),
			Rating:      r.Rating,
		}
	})}, nil
}
