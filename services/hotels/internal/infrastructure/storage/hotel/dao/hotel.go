package dao

import (
	entity "hotels/internal/domain/entity/hotel"
	"hotels/internal/pkg/xo"
)

type Hotel struct {
	xo.Hotel
}

func (row Hotel) ConvertTo() *entity.Hotel {
	return entity.With(row.ID,
		entity.WithName(row.Name.String),
		entity.WithDescription(row.Description.String),
		entity.WithStars(int(row.Stars.Int64)),
		entity.WithCountry(row.Country.String),
		entity.WithCity(row.City.String),
		entity.WithAddress(row.Address.String),
		entity.WithLatitude(row.Latitude.Float64),
		entity.WithLongitude(row.Longitude.Float64),
	)
}
