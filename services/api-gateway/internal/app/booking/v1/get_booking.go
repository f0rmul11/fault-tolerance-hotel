package booking

import (
	"context"

	"api-gateway/internal/pkg/auth"
	bookingV1 "api-gateway/internal/pkg/pb/api-gateway/booking/v1"
	externalV1 "api-gateway/internal/pkg/pb/external/booking/booking/v1"
)

func (i *Implementation) GetBooking(ctx context.Context, req *bookingV1.GetBookingRequest) (*bookingV1.GetBookingResponse, error) {
	response, err := i.external.GetBooking(ctx, &externalV1.GetBookingRequest{
		Context:   auth.Caller(ctx),
		BookingId: req.GetBookingId(),
	})
	if err != nil {
		return nil, err
	}

	booking := response.GetBooking()

	return &bookingV1.GetBookingResponse{
		Booking: &bookingV1.BookingView{
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
			EndsAt:     booking.GetEndsAt(),
		},
	}, nil
}
