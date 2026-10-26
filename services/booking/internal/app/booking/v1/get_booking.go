package booking

import (
	"context"
	"strconv"
	"time"

	"github.com/gofrs/uuid"

	"booking/internal/application/usecase/booking/get"
	"booking/internal/pkg/grpc/intercept"
	pb "booking/internal/pkg/pb/booking/booking/v1"
	"booking/internal/pkg/telemetry/traces"
)

func (s *Server) GetBooking(ctx context.Context, r *pb.GetBookingRequest) (*pb.GetBookingResponse, error) {
	ctx, span := traces.Start(ctx, "handler.booking.GetBooking")
	defer span.End()

	b, err := s.usecases.GetBooking.Execute(
		ctx,
		get.Input{
			Context:   intercept.Caller(ctx),
			BookingID: uuid.FromStringOrNil(r.GetBookingId()),
		},
	)
	if err != nil {
		return nil, err
	}
	return &pb.GetBookingResponse{Booking: &pb.BookingView{
		Id:         b.ID().String(),
		RoomId:     strconv.FormatInt(b.RoomID(), 10),
		RoomNumber: b.RoomNumber(),
		RoomType:   b.RoomType(),
		GuestId:    b.GuestID().String(),
		StartsAt:   b.Period().Start().Format(time.RFC3339Nano),
		EndsAt:     b.Period().End().Format(time.RFC3339Nano),
		Status:     pb.BookingView_Status(pb.BookingView_Status_value[string(b.Status())]),
		Version:    int32(b.Version()),
		CreatedAt:  b.CreatedAt().Format(time.RFC3339Nano),
		UpdatedAt:  b.UpdatedAt().Format(time.RFC3339Nano),
		RequestId:  b.RequestID().String(),
	}}, nil
}
