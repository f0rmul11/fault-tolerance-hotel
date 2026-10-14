package entity

import (
	"time"

	"github.com/gofrs/uuid"

	"booking/internal/domain/vo"
)

type Option func(*Booking)

func With(id uuid.UUID, opts ...Option) *Booking {
	value := &Booking{id: id}
	for _, opt := range opts {
		opt(value)
	}

	return value
}

func WithGuestID(value uuid.UUID) Option {
	return func(entity *Booking) {
		entity.guestID = value
	}
}

func WithRoomID(value int64) Option {
	return func(entity *Booking) {
		entity.roomID = value
	}
}

func WithPeriod(value vo.Period) Option {
	return func(entity *Booking) {
		entity.period = value
	}
}

func WithRequestID(value uuid.UUID) Option {
	return func(entity *Booking) {
		entity.requestID = value
	}
}

func WithStatus(value Status) Option {
	return func(entity *Booking) {
		entity.status = value
	}
}

func WithCreatedAt(value time.Time) Option {
	return func(entity *Booking) {
		entity.createdAt = value.UTC()
	}
}

func WithUpdatedAt(value time.Time) Option {
	return func(entity *Booking) {
		entity.updatedAt = value.UTC()
	}
}

func WithVersion(value int) Option {
	return func(b *Booking) {
		b.version = version{Old: value}
	}
}

func WithRoomNumber(value string) Option {
	return func(v *Booking) {
		v.roomNumber = value
	}
}

func WithRoomType(value string) Option {
	return func(v *Booking) {
		v.roomType = value
	}
}
