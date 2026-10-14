package booking

import (
	"booking/internal/pkg/bookingerror"
	"context"
	"strconv"
	"time"

	"github.com/gofrs/uuid"

	"booking/internal/application/usecase/booking/create"
	"booking/internal/domain/vo"
	"booking/internal/pkg/grpc/intercept"
	pb "booking/internal/pkg/pb/booking/booking/v1"
)

func (s *Server) CreateBooking(ctx context.Context, r *pb.CreateBookingRequest) (*pb.CreateBookingResponse, error) {
	roomID, err := strconv.ParseInt(r.GetRoomId(), 10, 64)
	if err != nil {
		return nil, bookingerror.NewInvalidArgumentErr("Идентификатор номера должен быть числом")
	}

	period, err := vo.ParsePeriod(r.GetStartsAt(), r.GetEndsAt())
	if err != nil {
		return nil, err
	}
	b, err := s.usecases.CreateBooking.Execute(
		ctx,
		create.Input{
			Context:        intercept.Caller(ctx),
			RoomID:         roomID,
			IdempotencyKey: uuid.FromStringOrNil(r.GetIdempotencyKey()),
			Period:         period,
		},
	)
	if err != nil {
		return nil, err
	}
	return &pb.CreateBookingResponse{Booking: &pb.BookingView{
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
