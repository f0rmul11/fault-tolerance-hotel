package get

import (
	"github.com/gofrs/uuid"

	bookingdto "booking/internal/dto/booking"
)

type Input struct {
	Context   bookingdto.Context
	BookingID uuid.UUID
}
