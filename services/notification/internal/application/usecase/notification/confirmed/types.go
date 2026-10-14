package confirmed

import (
	"github.com/gofrs/uuid"
	"time"
)

type Input struct {
	CorrelationID uuid.UUID
	BookingID     uuid.UUID
	RoomNumber    string
	RoomType      string
	StartsAt      time.Time
	EndsAt        time.Time
}
