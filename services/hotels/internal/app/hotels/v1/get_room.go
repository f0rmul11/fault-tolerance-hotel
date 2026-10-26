package hotels

import (
	"context"
	"strconv"
	"time"

	app "hotels/internal/pkg/hotelserror"
	"hotels/internal/pkg/telemetry/traces"

	"github.com/samber/lo"

	pb "hotels/internal/pkg/pb/hotels/hotels/v1"
)

func (s *Service) GetRoom(ctx context.Context, req *pb.GetRoomRequest) (*pb.GetRoomResponse, error) {
	ctx, span := traces.Start(ctx, "handler.hotels.GetRoom")
	defer span.End()

	id, err := strconv.ParseInt(req.GetRoomId(), 10, 64)
	if err != nil {
		return nil, app.NewInvalidArgumentErr("Идентификатор номера должен быть числом")
	}

	result, err := s.usecases.GetRoom.Execute(ctx, id)
	if err != nil {
		return nil, err
	}
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
	return &pb.GetRoomResponse{Room: view}, nil
}
