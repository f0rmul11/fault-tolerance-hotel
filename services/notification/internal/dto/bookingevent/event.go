package bookingevent

import (
	"encoding/json"

	"github.com/gofrs/uuid"
)

type Event struct {
	Type          string
	BookingID     uuid.UUID
	CorrelationID uuid.UUID
	Payload       json.RawMessage
}
