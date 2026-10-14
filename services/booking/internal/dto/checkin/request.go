package checkin

import (
	"time"

	"github.com/gofrs/uuid"

	app "booking/internal/pkg/bookingerror"
)

type Request struct {
	CorrelationID  uuid.UUID
	CheckInID      uuid.UUID
	BookingID      uuid.UUID
	CheckInVersion int
	ActorID        uuid.UUID
	RequestID      uuid.UUID
	RequestedAt    time.Time
}

func (r Request) Validate() error {
	if r.CorrelationID == uuid.Nil || r.CheckInID == uuid.Nil || r.BookingID == uuid.Nil {
		return app.NewInvalidArgumentErr("Не указаны идентификаторы события заселения")
	}
	if r.CheckInVersion < 1 || r.RequestedAt.IsZero() {
		return app.NewInvalidArgumentErr("Не указаны версия или время действия заселения")
	}
	return nil
}
