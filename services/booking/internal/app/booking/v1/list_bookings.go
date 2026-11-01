package booking

import (
	"booking/internal/application/usecase/booking/list"
	"booking/internal/pkg/grpc/intercept"
	pb "booking/internal/pkg/pb/booking/booking/v1"
	"context"
	"strconv"
	"time"
)

func (s *Server) ListBookings(ctx context.Context, r *pb.ListBookingsRequest) (*pb.ListBookingsResponse, error) {
	result, err := s.usecases.ListBookings.Execute(ctx, list.Input{Context: intercept.Caller(ctx)})
	if err != nil {
		return nil, err
	}
	response := &pb.ListBookingsResponse{
		BookingsVersion: result.Version.Current(),
		Bookings:        make([]*pb.BookingView, 0, len(result.Bookings)),
	}
	for _, b := range result.Bookings {
		response.Bookings = append(response.Bookings, &pb.BookingView{
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
		})
	}
	return response, nil
}
