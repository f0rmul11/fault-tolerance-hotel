package booking

import (
	"api-gateway/internal/pkg/auth"
	bookingV1 "api-gateway/internal/pkg/pb/api-gateway/booking/v1"
	externalV1 "api-gateway/internal/pkg/pb/external/booking/booking/v1"
	"context"
)

func (i *Implementation) ListBookings(ctx context.Context, req *bookingV1.ListBookingsRequest) (*bookingV1.ListBookingsResponse, error) {
	result, err := i.external.ListBookings(ctx, &externalV1.ListBookingsRequest{Context: auth.Caller(ctx)})
	if err != nil {
		return nil, err
	}
	response := &bookingV1.ListBookingsResponse{
		BookingsVersion: result.GetBookingsVersion(),
		Bookings:        make([]*bookingV1.BookingView, 0, len(result.GetBookings())),
	}
	for _, booking := range result.GetBookings() {
		response.Bookings = append(response.Bookings, &bookingV1.BookingView{
			Id:         booking.GetId(),
			Version:    booking.GetVersion(),
			CreatedAt:  booking.GetCreatedAt(),
			UpdatedAt:  booking.GetUpdatedAt(),
			GuestId:    booking.GetGuestId(),
			Status:     booking.GetStatus().String(),
			RequestId:  booking.GetRequestId(),
			RoomId:     booking.GetRoomId(),
			RoomNumber: booking.GetRoomNumber(),
			RoomType:   booking.GetRoomType(),
			StartsAt:   booking.GetStartsAt(),
			EndsAt:     booking.GetEndsAt()})
	}
	return response, nil
}
