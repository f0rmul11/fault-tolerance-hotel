package entity

import (
	"time"

	"github.com/gofrs/uuid"

	"check-in/internal/domain/vo"
)

type Option func(*CheckIn)

func With(id uuid.UUID, opts ...Option) *CheckIn {
	c := &CheckIn{id: id}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func WithVersion(value int) Option {
	return func(c *CheckIn) {
		c.version = version{Old: value}
	}
}

func WithBookingID(value uuid.UUID) Option {
	return func(c *CheckIn) {
		c.bookingID = value
	}
}

func WithRoomID(value int64) Option {
	return func(c *CheckIn) {
		c.roomID = value
	}
}

func WithGuestID(value uuid.UUID) Option {
	return func(c *CheckIn) {
		c.guestID = value
	}
}

func WithPeriod(value vo.Period) Option {
	return func(c *CheckIn) {
		c.period = value
	}
}

func WithStatus(value Status) Option {
	return func(c *CheckIn) {
		c.status = value
	}
}

func WithPreviousStatus(value Status) Option {
	return func(c *CheckIn) {
		c.previousStatus = value
	}
}

func WithActorID(value uuid.UUID) Option {
	return func(c *CheckIn) {
		c.actorID = value
	}
}

func WithRequestID(value uuid.UUID) Option {
	return func(c *CheckIn) {
		c.requestID = value
	}
}

func WithRequestedAt(value time.Time) Option {
	return func(c *CheckIn) {
		c.requestedAt = value
	}
}

func WithFailure(value string) Option {
	return func(c *CheckIn) {
		c.failure = value
	}
}

func WithRoomNumber(value string) Option {
	return func(v *CheckIn) {
		v.roomNumber = value
	}
}

func WithRoomType(value string) Option {
	return func(v *CheckIn) {
		v.roomType = value
	}
}
