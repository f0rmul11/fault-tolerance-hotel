package dao

import (
	entity "review/internal/domain/entity/rating"
	"review/internal/pkg/xo"
)

type Rating struct {
	xo.Rating
}

func (row Rating) ConvertTo() *entity.Rating {
	return entity.With(row.HotelID,
		entity.WithTotalStars(row.TotalStars.Int64),
		entity.WithReviewCount(row.ReviewCount.Int64),
	)
}
