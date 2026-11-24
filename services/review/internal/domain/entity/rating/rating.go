package rating

import (
	"time"

	"github.com/gofrs/uuid"

	app "review/internal/pkg/reviewerror"
)

type Rating struct {
	id          uuid.UUID
	totalStars  int64
	reviewCount int64
	version     version
	updatedAt   time.Time
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

// New создаёт пустой агрегат; первая оценка устанавливает версию 1.
func New(id uuid.UUID) *Rating {
	return &Rating{id: id}
}

func (v *Rating) Add(stars int64, at time.Time) error {
	if stars < 1 || stars > 5 {
		return app.NewInvalidArgumentErr("Оценка должна быть целым числом от 1 до 5")
	}
	v.totalStars += stars
	v.reviewCount++
	v.version.inc()
	v.updatedAt = at
	return nil
}

func (v *Rating) UpdatedAt() time.Time {
	return v.updatedAt
}
