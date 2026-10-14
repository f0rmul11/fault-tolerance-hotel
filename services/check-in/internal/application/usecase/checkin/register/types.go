package register

import (
	"github.com/gofrs/uuid"

	"check-in/internal/domain/vo"
)

type Input struct {
	BookingID  uuid.UUID
	RoomID     int64
	RoomNumber string
	RoomType   string
	GuestID    uuid.UUID
	Period     vo.Period
}
