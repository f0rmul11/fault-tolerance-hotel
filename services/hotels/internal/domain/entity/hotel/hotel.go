package hotel

import (
	"github.com/gofrs/uuid"

	dto "hotels/internal/dto/hotel"
)

type Hotel struct {
	id          uuid.UUID
	name        string
	description string
	stars       int
	country     string
	city        string
	address     string
	latitude    float64
	longitude   float64
	rating      dto.Rating
}

func (v *Hotel) ID() uuid.UUID {
	return v.id
}

func (v *Hotel) Name() string {
	return v.name
}

func (v *Hotel) Description() string {
	return v.description
}

func (v *Hotel) Stars() int {
	return v.stars
}

func (v *Hotel) Country() string {
	return v.country
}

func (v *Hotel) City() string {
	return v.city
}

func (v *Hotel) Address() string {
	return v.address
}

func (v *Hotel) Latitude() float64 {
	return v.latitude
}

func (v *Hotel) Longitude() float64 {
	return v.longitude
}

func (v *Hotel) Rating() dto.Rating {
	return v.rating
}

func (v *Hotel) SetRating(rating dto.Rating) {
	v.rating = rating
}
