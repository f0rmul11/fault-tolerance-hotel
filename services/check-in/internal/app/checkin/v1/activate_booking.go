package checkin

import (
	"context"
	"strconv"
	"time"

	"github.com/gofrs/uuid"

	"check-in/internal/application/usecase/checkin/activate"
	"check-in/internal/pkg/grpc/intercept"
	pb "check-in/internal/pkg/pb/check-in/checkin/v1"
)

func (s *Server) ActivateBooking(ctx context.Context, r *pb.ActivateBookingRequest) (*pb.ActivateBookingResponse, error) {
	c, err := s.usecases.ActivateCheckIn.Execute(
		ctx,
		activate.Input{
			Context:        intercept.Caller(ctx),
			BookingID:      uuid.FromStringOrNil(r.GetBookingId()),
			IdempotencyKey: uuid.FromStringOrNil(r.GetIdempotencyKey()),
		},
	)
	if err != nil {
		return nil, err
	}
	return &pb.ActivateBookingResponse{Checkin: &pb.CheckInView{
		Id:         c.ID().String(),
		BookingId:  c.BookingID().String(),
		RoomId:     strconv.FormatInt(c.RoomID(), 10),
		RoomNumber: c.RoomNumber(),
		RoomType:   c.RoomType(),
		GuestId:    c.GuestID().String(),
		StartsAt:   c.Period().Start().Format(time.RFC3339Nano),
		EndsAt:     c.Period().End().Format(time.RFC3339Nano),
		Status:     string(c.Status()),
		Version:    int32(c.Version()),
		Failure:    c.Failure(),
	}}, nil
}
