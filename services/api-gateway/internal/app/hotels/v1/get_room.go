package hotels

import (
	pb "api-gateway/internal/pkg/pb/api-gateway/hotels/v1"
	external "api-gateway/internal/pkg/pb/external/hotels/hotels/v1"
	"api-gateway/internal/pkg/telemetry/traces"
	"context"
)

func (i *Implementation) GetRoom(ctx context.Context, req *pb.GetRoomRequest) (*pb.GetRoomResponse, error) {
	ctx, span := traces.Start(ctx, "handler.hotels.GetRoom")
	defer span.End()

	response, err := i.external.GetRoom(ctx, &external.GetRoomRequest{RoomId: req.GetRoomId()})
	if err != nil {
		return nil, err
	}
	r := response.GetRoom()
	return &pb.GetRoomResponse{Room: &pb.Room{
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
	}}, nil
}
