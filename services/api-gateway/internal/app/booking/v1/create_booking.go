package booking

import (
	"context"

	"api-gateway/internal/pkg/auth"
	bookingV1 "api-gateway/internal/pkg/pb/api-gateway/booking/v1"
	externalV1 "api-gateway/internal/pkg/pb/external/booking/booking/v1"
	"api-gateway/internal/pkg/telemetry/traces"
)

func (i *Implementation) CreateBooking(ctx context.Context, req *bookingV1.CreateBookingRequest) (*bookingV1.CreateBookingResponse, error) {
	ctx, span := traces.Start(ctx, "handler.booking.CreateBooking")
	defer span.End()

	response, err := i.external.CreateBooking(ctx, &externalV1.CreateBookingRequest{
		Context:        auth.Caller(ctx),
		IdempotencyKey: req.GetIdempotencyKey(),
		RoomId:         req.GetRoomId(),
		StartsAt:       req.GetStartsAt(),
		EndsAt:         req.GetEndsAt(),
	})
	if err != nil {
		return nil, err
	}

	booking := response.GetBooking()

	return &bookingV1.CreateBookingResponse{
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
