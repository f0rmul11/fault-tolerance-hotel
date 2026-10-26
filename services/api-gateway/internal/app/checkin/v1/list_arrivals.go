package checkin

import (
	"context"

	"github.com/samber/lo"

	"api-gateway/internal/pkg/auth"
	pb "api-gateway/internal/pkg/pb/api-gateway/checkin/v1"
	external "api-gateway/internal/pkg/pb/external/check-in/checkin/v1"
	"api-gateway/internal/pkg/telemetry/traces"
)

func (s *Implementation) ListArrivals(ctx context.Context, r *pb.ListArrivalsRequest) (*pb.ListArrivalsResponse, error) {
	ctx, span := traces.Start(ctx, "handler.checkin.ListArrivals")
	defer span.End()

	caller := auth.Caller(ctx)
	response, err := s.external.ListArrivals(ctx, &external.ListArrivalsRequest{
		Context: &external.RequestContext{
			RequestId: caller.RequestId,
			DemoActor: caller.DemoActor,
			DemoRole:  external.RequestContext_DemoRole(caller.GetDemoRole()).Enum(),
		},
		StartsAt: r.GetStartsAt(),
		EndsAt:   r.GetEndsAt(),
	})
	if err != nil {
		return nil, err
	}
	return &pb.ListArrivalsResponse{Checkins: lo.Map(response.GetCheckins(), func(c *external.CheckInView, _ int) *pb.CheckInView {
		return &pb.CheckInView{
			Id:         c.GetId(),
			BookingId:  c.GetBookingId(),
			RoomId:     c.GetRoomId(),
			RoomNumber: c.GetRoomNumber(),
			RoomType:   c.GetRoomType(),
			GuestId:    c.GetGuestId(),
			StartsAt:   c.GetStartsAt(),
			EndsAt:     c.GetEndsAt(),
			Status:     c.GetStatus(),
			Version:    c.GetVersion(),
			Failure:    c.GetFailure(),
		}
	})}, nil
}
