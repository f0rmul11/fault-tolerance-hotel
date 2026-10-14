package booking

import (
	bookingV1 "api-gateway/internal/pkg/pb/api-gateway/booking/v1"
	externalV1 "api-gateway/internal/pkg/pb/external/booking/booking/v1"
)

type Implementation struct {
	bookingV1.UnimplementedBookingServiceServer
	external externalV1.BookingServiceClient
}

func NewBookingService(external externalV1.BookingServiceClient) *Implementation {
	return &Implementation{
		external: external,
	}
}
