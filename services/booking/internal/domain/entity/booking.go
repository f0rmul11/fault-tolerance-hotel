package entity

import (
	"time"

	"github.com/gofrs/uuid"

	"booking/internal/domain/vo"
	app "booking/internal/pkg/bookingerror"
)

type Booking struct {
	id         uuid.UUID
	guestID    uuid.UUID
	roomID     int64
	roomNumber string
	roomType   string
	period     vo.Period
	requestID  uuid.UUID
	status     Status
	version    version
	createdAt  time.Time
	updatedAt  time.Time
}

func NewBooking(id, guestID uuid.UUID, roomID int64, requestID uuid.UUID, period vo.Period, now time.Time) (*Booking, error) {
	if id == uuid.Nil || guestID == uuid.Nil || requestID == uuid.Nil {
		return nil, app.NewInvalidArgumentErr("Не указаны идентификаторы брони, гостя или запроса")
	}
	if roomID <= 0 {
		return nil, app.NewInvalidArgumentErr("Идентификатор номера должен быть положительным числом")
	}

	if now.IsZero() {
		return nil, app.NewInvalidArgumentErr("Не указано время создания брони")
	}

	if err := period.Validate(); err != nil {
		return nil, err
	}

	b := &Booking{
		id:        id,
		guestID:   guestID,
		roomID:    roomID,
		requestID: requestID,
		period:    period,
		status:    Confirmed,
		version:   version{Old: 1},
		createdAt: now.UTC(),
		updatedAt: now.UTC(),
	}

	return b, nil
}

// Replay возвращает результат той же операции, даже если request_id у повтора другой.
func (b *Booking) Replay(request *Booking) (*Booking, error) {
	if b.guestID != request.guestID || b.roomID != request.roomID || !b.period.Equal(request.period) {
		return nil, app.NewConflictErr("Ключ идемпотентности уже использован для другой брони")
	}

	return b, nil
}

func (b *Booking) OldVersion() int {
	return b.version.Old
}

func (b *Booking) Version() int {
	return b.version.current()
}

func (b *Booking) ID() uuid.UUID {
	return b.id
}

func (b *Booking) GuestID() uuid.UUID {
	return b.guestID
}

func (b *Booking) RoomID() int64 {
	return b.roomID
}

func (b *Booking) Period() vo.Period {
	return b.period
}

func (b *Booking) RequestID() uuid.UUID {
	return b.requestID
}

func (b *Booking) Status() Status {
	return b.status
}

func (b *Booking) CreatedAt() time.Time {
	return b.createdAt
}

func (b *Booking) UpdatedAt() time.Time {
	return b.updatedAt
}

func (b *Booking) Activate(at time.Time) (bool, error) {
	if b.status == Active {
		return false, nil
	}
	if b.status != Confirmed {
		return false, app.NewBusinessErr("Активировать можно только подтверждённую бронь")
	}
	if at.Before(b.period.Start()) || !at.Before(b.period.End()) {
		return false, app.NewBusinessErr("Время прибытия находится вне периода бронирования")
	}
	b.status = Active
	b.updatedAt = at.UTC()
	b.version.inc()
	return true, nil
}

func (b *Booking) Cancel(at time.Time) (bool, error) {
	if b.status == Canceled {
		return false, nil
	}
	if b.status != Confirmed && b.status != Active {
		return false, app.NewBusinessErr("Отмена брони недоступна")
	}
	b.status = Canceled
	b.updatedAt = at.UTC()
	b.version.inc()
	return true, nil
}

// SetRoomDetails сохраняет снимок данных номера при создании брони.
func (b *Booking) SetRoomDetails(number, roomType string) {
	b.roomNumber = number
	b.roomType = roomType
}

func (b *Booking) RoomNumber() string {
	return b.roomNumber
}

func (b *Booking) RoomType() string {
	return b.roomType
}
