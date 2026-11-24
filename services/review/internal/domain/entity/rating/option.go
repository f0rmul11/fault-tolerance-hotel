package rating

import (
	"time"

	"github.com/gofrs/uuid"
)

type Option func(*Rating)

func With(id uuid.UUID, opts ...Option) *Rating {
	v := &Rating{id: id}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

func WithTotalStars(value int64) Option {
	return func(v *Rating) {
		v.totalStars = value
	}
}

func WithReviewCount(value int64) Option {
	return func(v *Rating) {
		v.reviewCount = value
	}
}

func WithVersion(value int64) Option {
	return func(v *Rating) {
		v.version = version{Old: value}
	}
}

func WithUpdatedAt(value time.Time) Option {
	return func(v *Rating) {
		v.updatedAt = value
	}
}
