package hotels

import (
	pb "api-gateway/internal/pkg/pb/api-gateway/hotels/v1"
	external "api-gateway/internal/pkg/pb/external/hotels/hotels/v1"
	"context"
)

func (i *Implementation) GetHotel(ctx context.Context, req *pb.GetHotelRequest) (*pb.GetHotelResponse, error) {
	response, err := i.external.GetHotel(ctx, &external.GetHotelRequest{HotelId: req.GetHotelId()})
	if err != nil {
		return nil, err
	}
	r := response.GetHotel()
	return &pb.GetHotelResponse{Hotel: &pb.Hotel{
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
	}}, nil
}
