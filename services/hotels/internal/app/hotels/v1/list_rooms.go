package hotels

import (
	"context"
	"strconv"
	"time"

	"github.com/gofrs/uuid"
	"github.com/samber/lo"

	entity "hotels/internal/domain/entity/room"
	pb "hotels/internal/pkg/pb/hotels/hotels/v1"
)

func (s *Service) ListRooms(ctx context.Context, req *pb.ListRoomsRequest) (*pb.ListRoomsResponse, error) {
	result, err := s.usecases.ListRooms.Execute(ctx, uuid.FromStringOrNil(req.GetHotelId()))
	if err != nil {
		return nil, err
	}
	views := lo.Map(result, func(result *entity.Room, _ int) *pb.Room {
		r := result
		view := &pb.Room{
			Id:                 strconv.FormatInt(r.ID(), 10),
			HotelId:            r.HotelID().String(),
			Number:             r.Number(),
			Name:               r.Name(),
			RoomType:           r.RoomType(),
			Description:        r.Description(),
			Capacity:           int32(r.Capacity()),
			RoomCount:          int32(r.RoomCount()),
			BedCount:           int32(r.BedCount()),
			AreaSquareMeters:   int32(r.AreaSquareMeters()),
			Amenities:          r.Amenities(),
			MinDurationSeconds: int64(r.MinDuration() / time.Second),
		}
		if r.MaxDuration() != 0 {
			view.MaxDurationSeconds = lo.ToPtr(int64(r.MaxDuration() / time.Second))
		}
		return view
	})
	return &pb.ListRoomsResponse{Rooms: views}, nil
}
