package booking

import (
	"context"

	"api-gateway/internal/pkg/auth"
	bookingV1 "api-gateway/internal/pkg/pb/api-gateway/booking/v1"
	externalV1 "api-gateway/internal/pkg/pb/external/booking/booking/v1"
)

func (i *Implementation) CheckAvailability(ctx context.Context, req *bookingV1.CheckAvailabilityRequest) (*bookingV1.CheckAvailabilityResponse, error) {
	response, err := i.external.CheckAvailability(ctx, &externalV1.CheckAvailabilityRequest{
		Context:  auth.Caller(ctx),
		HotelId:  req.GetHotelId(),
		StartsAt: req.GetStartsAt(),
		EndsAt:   req.GetEndsAt(),
	})
	if err != nil {
		return nil, err
	}

	return &bookingV1.CheckAvailabilityResponse{RoomIds: response.GetRoomIds()}, nil
}
