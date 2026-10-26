package booking

import (
	"context"
	"strconv"

	"github.com/gofrs/uuid"
	"github.com/samber/lo"

	"booking/internal/application/usecase/booking/available"
	"booking/internal/domain/vo"
	pb "booking/internal/pkg/pb/booking/booking/v1"
	"booking/internal/pkg/telemetry/traces"
)

func (s *Server) CheckAvailability(ctx context.Context, r *pb.CheckAvailabilityRequest) (*pb.CheckAvailabilityResponse, error) {
	ctx, span := traces.Start(ctx, "handler.booking.CheckAvailability")
	defer span.End()

	period, err := vo.ParsePeriod(r.GetStartsAt(), r.GetEndsAt())
	if err != nil {
		return nil, err
	}
	ids, err := s.usecases.CheckAvailability.Execute(
		ctx,
		available.Input{
			HotelID: uuid.FromStringOrNil(r.GetHotelId()),
			Period:  period,
		},
	)
	if err != nil {
		return nil, err
	}
	return &pb.CheckAvailabilityResponse{RoomIds: lo.Map(ids, func(id int64, _ int) string {
		return strconv.FormatInt(id, 10)
	})}, nil
}
