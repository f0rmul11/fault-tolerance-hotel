package hotels

import (
	"context"

	"github.com/gofrs/uuid"
	"github.com/samber/lo"

	pb "hotels/internal/pkg/pb/hotels/hotels/v1"
	"hotels/internal/pkg/telemetry/traces"
)

func (s *Service) GetHotel(ctx context.Context, req *pb.GetHotelRequest) (*pb.GetHotelResponse, error) {
	ctx, span := traces.Start(ctx, "handler.hotels.GetHotel")
	defer span.End()

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
