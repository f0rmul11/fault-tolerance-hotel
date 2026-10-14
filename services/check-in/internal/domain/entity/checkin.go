package entity

import (
	"time"

	"github.com/gofrs/uuid"

	"check-in/internal/domain/vo"
	app "check-in/internal/pkg/checkinerror"
)

// CheckIn — ожидаемое прибытие гостя и его оформление на ресепшн.
type CheckIn struct {
	id             uuid.UUID
	version        version
	bookingID      uuid.UUID
	roomID         int64
	roomNumber     string
	roomType       string
	guestID        uuid.UUID
	period         vo.Period
	status         Status
	previousStatus Status
	actorID        uuid.UUID
	requestID      uuid.UUID
	requestedAt    time.Time
	failure        string
}

func NewCheckIn(id, bookingID uuid.UUID, roomID int64, guestID uuid.UUID, period vo.Period) (*CheckIn, error) {
	if id == uuid.Nil || bookingID == uuid.Nil || guestID == uuid.Nil {
		return nil, app.NewInvalidArgumentErr("Не указаны идентификаторы заселения")
	}
	if roomID <= 0 {
		return nil, app.NewInvalidArgumentErr("Идентификатор номера должен быть положительным числом")
	}

	if err := period.Validate(); err != nil {
		return nil, err
	}
	return &CheckIn{
		id:        id,
		bookingID: bookingID,
		roomID:    roomID,
		guestID:   guestID,
		period:    period,
		status:    Expected,
		version:   version{Old: 1},
	}, nil
}

func (c *CheckIn) Arrive(actorID, requestID uuid.UUID, at time.Time) error {
	if c.status != Expected {
		return app.NewBusinessErr("Заселение уже начато или недоступно")
	}
	if at.Before(c.period.Start()) || !at.Before(c.period.End()) {
		return app.NewBusinessErr("Время прибытия находится вне периода проживания")
	}
	c.previousStatus = c.status
	c.status = Arriving
	c.actorID = actorID
	c.requestID = requestID
	c.requestedAt = at.UTC().Truncate(time.Microsecond)
	c.failure = ""
	c.version.inc()
	return nil
}

func (c *CheckIn) Cancel(actorID, requestID uuid.UUID, at time.Time) error {
	if c.status != Expected && c.status != CheckedIn {
		return app.NewBusinessErr("Отмена уже начата или недоступна")
	}
	c.previousStatus = c.status
	c.status = Canceling
	c.actorID = actorID
	c.requestID = requestID
	c.requestedAt = at.UTC().Truncate(time.Microsecond)
	c.failure = ""
	c.version.inc()
	return nil
}

func (c *CheckIn) ConfirmArrival(requestVersion int) bool {
	if c.Version() != requestVersion || c.status != Arriving {
		return false
	}
	c.status = CheckedIn
	c.previousStatus = ""
	c.version.inc()
	return true
}

func (c *CheckIn) ConfirmCancellation(requestVersion int) bool {
	if c.Version() != requestVersion || c.status != Canceling {
		return false
	}
	c.status = Canceled
	c.previousStatus = ""
	c.version.inc()
	return true
}

func (c *CheckIn) Reject(requestVersion int, message string) bool {
	if c.Version() != requestVersion || (c.status != Arriving && c.status != Canceling) {
		return false
	}
	c.status = c.previousStatus
	c.previousStatus = ""
	c.failure = message
	c.version.inc()
	return true
}

func (c *CheckIn) ID() uuid.UUID {
	return c.id
}

func (c *CheckIn) Version() int {
	return c.version.current()
}

func (c *CheckIn) OldVersion() int {
	return c.version.Old
}

func (c *CheckIn) BookingID() uuid.UUID {
	return c.bookingID
}

func (c *CheckIn) RoomID() int64 {
	return c.roomID
}

func (c *CheckIn) GuestID() uuid.UUID {
	return c.guestID
}

func (c *CheckIn) Period() vo.Period {
	return c.period
}

func (c *CheckIn) Status() Status {
	return c.status
}

func (c *CheckIn) PreviousStatus() Status {
	return c.previousStatus
}

func (c *CheckIn) ActorID() uuid.UUID {
	return c.actorID
}

func (c *CheckIn) RequestID() uuid.UUID {
	return c.requestID
}

func (c *CheckIn) RequestedAt() time.Time {
	return c.requestedAt
}

func (c *CheckIn) Failure() string {
	return c.failure
}

// SetRoomDetails сохраняет снимок данных номера при создании заселения.
func (c *CheckIn) SetRoomDetails(number, roomType string) {
	c.roomNumber = number
	c.roomType = roomType
}

func (c *CheckIn) RoomNumber() string {
	return c.roomNumber
}

func (c *CheckIn) RoomType() string {
	return c.roomType
}
