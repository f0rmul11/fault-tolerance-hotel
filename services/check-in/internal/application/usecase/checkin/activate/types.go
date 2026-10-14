package activate

import (
	"github.com/gofrs/uuid"

	checkindto "check-in/internal/dto/checkin"
)

type Input struct {
	Context        checkindto.Context
	BookingID      uuid.UUID
	IdempotencyKey uuid.UUID
}
