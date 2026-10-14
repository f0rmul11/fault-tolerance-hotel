package reject

import "github.com/gofrs/uuid"

type Input struct {
	CheckInID      uuid.UUID
	BookingID      uuid.UUID
	CheckInVersion int
	Message        string
}
