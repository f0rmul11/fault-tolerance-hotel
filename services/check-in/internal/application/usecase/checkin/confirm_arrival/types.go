package confirm_arrival

import "github.com/gofrs/uuid"

type Input struct {
	CheckInID      uuid.UUID
	BookingID      uuid.UUID
	CheckInVersion int
}
