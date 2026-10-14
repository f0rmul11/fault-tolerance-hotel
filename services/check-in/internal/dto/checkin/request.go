package checkin

import (
	"github.com/gofrs/uuid"

	"check-in/internal/domain/entity"
)

// Request — ключ повторного HTTP-запроса, без собственного жизненного цикла.
type Request struct {
	ActorID   uuid.UUID
	Key       uuid.UUID
	BookingID uuid.UUID
	Action    entity.Action
}

func (r Request) Matches(bookingID uuid.UUID, action entity.Action) bool {
	return r.BookingID == bookingID && r.Action == action
}
