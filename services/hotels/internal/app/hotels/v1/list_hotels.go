package hotels

import (
	"context"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"hotels/internal/application/usecase/hotel/list"
	entity "hotels/internal/domain/entity/hotel"
	pb "hotels/internal/pkg/pb/hotels/hotels/v1"
)

func (s *Service) ListHotels(ctx context.Context, req *pb.ListHotelsRequest) (*pb.ListHotelsResponse, error) {
	limit := mo.None[int]()
	if req.Limit != nil {
		limit = mo.Some(int(req.GetLimit()))
	}
	result, err := s.usecases.ListHotels.Execute(ctx, list.Input{
		Limit:  limit,
		Offset: int(req.GetOffset()),
	})
	if err != nil {
		return nil, err
	}
	views := lo.Map(result, func(h *entity.Hotel, _ int) *pb.Hotel {
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
		return view
	})
	return &pb.ListHotelsResponse{Hotels: views}, nil
}
