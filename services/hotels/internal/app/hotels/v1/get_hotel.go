package hotels

import (
	"context"

	"github.com/gofrs/uuid"
	"github.com/samber/lo"

	pb "hotels/internal/pkg/pb/hotels/hotels/v1"
)

func (s *Service) GetHotel(ctx context.Context, req *pb.GetHotelRequest) (*pb.GetHotelResponse, error) {
	h, err := s.usecases.GetHotel.Execute(ctx, uuid.FromStringOrNil(req.GetHotelId()))
	if err != nil {
		return nil, err
	}
	rating := h.Rating()
	view := &pb.Hotel{
		Id:          h.ID().String(),
		Name:        h.Name(),
		Description: h.Description(),
		Stars:       int32(h.Stars()),
		Country:     h.Country(),
		City:        h.City(),
		Address:     h.Address(),
		Latitude:    h.Latitude(),
		Longitude:   h.Longitude(),
		ReviewCount: rating.ReviewCount,
	}
	if rating.ReviewCount > 0 {
		view.Rating = lo.ToPtr(rating.Value)
	}
	return &pb.GetHotelResponse{Hotel: view}, nil
}
