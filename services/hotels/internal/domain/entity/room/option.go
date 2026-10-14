package room

import (
	"slices"
	"time"

	"github.com/gofrs/uuid"
)

type Option func(*Room)

func With(id int64, opts ...Option) *Room {
	v := &Room{id: id}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

func WithHotelID(value uuid.UUID) Option {
	return func(v *Room) {
		v.hotelID = value
	}
}

func WithNumber(value string) Option {
	return func(v *Room) {
		v.number = value
	}
}

func WithName(value string) Option {
	return func(v *Room) {
		v.name = value
	}
}

func WithRoomType(value string) Option {
	return func(v *Room) {
		v.roomType = value
	}
}

func WithDescription(value string) Option {
	return func(v *Room) {
		v.description = value
	}
}

func WithCapacity(value int) Option {
	return func(v *Room) {
		v.capacity = value
	}
}

func WithRoomCount(value int) Option {
	return func(v *Room) {
		v.roomCount = value
	}
}

func WithBedCount(value int) Option {
	return func(v *Room) {
		v.bedCount = value
	}
}

func WithAreaSquareMeters(value int) Option {
	return func(v *Room) {
		v.areaSquareMeters = value
	}
}

func WithAmenities(value []string) Option {
	return func(v *Room) {
		v.amenities = slices.Clone(value)
	}
}

func WithMinDuration(value time.Duration) Option {
	return func(v *Room) {
		v.minDuration = value
	}
}

func WithMaxDuration(value time.Duration) Option {
	return func(v *Room) {
		v.maxDuration = value
	}
}
