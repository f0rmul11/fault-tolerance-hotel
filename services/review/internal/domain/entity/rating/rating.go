package rating

import (
	"github.com/gofrs/uuid"
)

type Rating struct {
	id          uuid.UUID
	totalStars  int64
	reviewCount int64
}

func (v *Rating) ID() uuid.UUID {
	return v.id
}

func (v *Rating) TotalStars() int64 {
	return v.totalStars
}

func (v *Rating) ReviewCount() int64 {
	return v.reviewCount
}

func (v *Rating) Value() float64 {
	if v.reviewCount == 0 {
		return 0
	}
	return float64(v.totalStars) / float64(v.reviewCount)
}
