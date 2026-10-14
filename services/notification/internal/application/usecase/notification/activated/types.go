package activated

import (
	"github.com/gofrs/uuid"
)

type Input struct {
	CorrelationID uuid.UUID
	BookingID     uuid.UUID
}
