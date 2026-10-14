package create

import (
	"github.com/gofrs/uuid"

	"booking/internal/domain/vo"
	bookingdto "booking/internal/dto/booking"
)

type Input struct {
	IdempotencyKey uuid.UUID
	Context        bookingdto.Context
	RoomID         int64
	Period         vo.Period
}
