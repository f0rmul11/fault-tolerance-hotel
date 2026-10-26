package checkin

import (
	"context"

	"api-gateway/internal/pkg/auth"
	pb "api-gateway/internal/pkg/pb/api-gateway/checkin/v1"
	external "api-gateway/internal/pkg/pb/external/check-in/checkin/v1"
	"api-gateway/internal/pkg/telemetry/traces"
)

func (s *Implementation) ActivateBooking(ctx context.Context, r *pb.ActivateBookingRequest) (*pb.ActivateBookingResponse, error) {
	ctx, span := traces.Start(ctx, "handler.checkin.ActivateBooking")
	defer span.End()

	caller := auth.Caller(ctx)
	response, err := s.external.ActivateBooking(ctx, &external.ActivateBookingRequest{
		Context: &external.RequestContext{
			RequestId: caller.RequestId,
			DemoActor: caller.DemoActor,
			DemoRole:  external.RequestContext_DemoRole(caller.GetDemoRole()).Enum(),
		},
		BookingId:      r.GetBookingId(),
		IdempotencyKey: r.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, err
	}
	c := response.GetCheckin()
	return &pb.ActivateBookingResponse{Checkin: &pb.CheckInView{
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
	}}, nil
}
