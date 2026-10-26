package hotels

import (
	pb "api-gateway/internal/pkg/pb/api-gateway/hotels/v1"
	external "api-gateway/internal/pkg/pb/external/hotels/hotels/v1"
	"api-gateway/internal/pkg/telemetry/traces"
	"context"

	"github.com/samber/lo"
)

func (i *Implementation) ListRooms(ctx context.Context, req *pb.ListRoomsRequest) (*pb.ListRoomsResponse, error) {
	ctx, span := traces.Start(ctx, "handler.hotels.ListRooms")
	defer span.End()

	response, err := i.external.ListRooms(ctx, &external.ListRoomsRequest{HotelId: req.GetHotelId()})
	if err != nil {
		return nil, err
	}
	return &pb.ListRoomsResponse{Rooms: lo.Map(response.GetRooms(), func(r *external.Room, _ int) *pb.Room {
		return &pb.Room{
			Id:                 r.GetId(),
			HotelId:            r.GetHotelId(),
			Number:             r.GetNumber(),
			Name:               r.GetName(),
			RoomType:           r.GetRoomType(),
			Description:        r.GetDescription(),
			Capacity:           r.GetCapacity(),
			RoomCount:          r.GetRoomCount(),
			BedCount:           r.GetBedCount(),
			AreaSquareMeters:   r.GetAreaSquareMeters(),
			Amenities:          r.GetAmenities(),
			MinDurationSeconds: r.GetMinDurationSeconds(),
			MaxDurationSeconds: r.MaxDurationSeconds,
		}
	})}, nil
}
