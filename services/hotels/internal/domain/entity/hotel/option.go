package hotel

import (
	"github.com/gofrs/uuid"
)

type Option func(*Hotel)

func With(id uuid.UUID, opts ...Option) *Hotel {
	v := &Hotel{id: id}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

func WithName(value string) Option {
	return func(v *Hotel) {
		v.name = value
	}
}

func WithDescription(value string) Option {
	return func(v *Hotel) {
		v.description = value
	}
}

func WithStars(value int) Option {
	return func(v *Hotel) {
		v.stars = value
	}
}

func WithCountry(value string) Option {
	return func(v *Hotel) {
		v.country = value
	}
}

func WithCity(value string) Option {
	return func(v *Hotel) {
		v.city = value
	}
}

func WithAddress(value string) Option {
	return func(v *Hotel) {
		v.address = value
	}
}

func WithLatitude(value float64) Option {
	return func(v *Hotel) {
		v.latitude = value
	}
}

func WithLongitude(value float64) Option {
	return func(v *Hotel) {
		v.longitude = value
	}
}
