package payload

import (
	"time"

	"github.com/gofrs/uuid"

	entity "hotels/internal/domain/entity/hotel"
	dto "hotels/internal/dto/hotel"
)

//go:generate go run github.com/mailru/easyjson/easyjson@v0.9.0 -all payload.go

// Hotel — формат полного снимка в кеше. JSON-теги остаются внутри адаптера.
//
//easyjson:json
type Hotel struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Stars       int     `json:"stars"`
	Country     string  `json:"country"`
	City        string  `json:"city"`
	Address     string  `json:"address"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`

	Rating        float64 `json:"rating"`
	ReviewCount   int64   `json:"review_count"`
	RatingVersion int64   `json:"rating_version"`

	// Deadlines относятся к этому снимку и не сдвигаются при неудачном refresh.
	SoftDeadline time.Time `json:"soft_deadline"`
	HardDeadline time.Time `json:"hard_deadline"`
}

// CanServe проверяет срок снимка и минимальную версию, запрошенную клиентом.
func (p Hotel) CanServe(now time.Time, minRatingVersion int64) bool {
	if p.SoftDeadline.IsZero() || !p.SoftDeadline.Before(p.HardDeadline) {
		return false
	}
	if !now.Before(p.HardDeadline) {
		return false
	}

	return p.RatingVersion >= minRatingVersion
}

// ConvertFrom вызывается после обогащения отеля рейтингом из Review.
func ConvertFrom(hotel *entity.Hotel) Hotel {
	return Hotel{
		ID:            hotel.ID().String(),
		Name:          hotel.Name(),
		Description:   hotel.Description(),
		Stars:         hotel.Stars(),
		Country:       hotel.Country(),
		City:          hotel.City(),
		Address:       hotel.Address(),
		Latitude:      hotel.Latitude(),
		Longitude:     hotel.Longitude(),
		Rating:        hotel.Rating().Value,
		ReviewCount:   hotel.Rating().ReviewCount,
		RatingVersion: hotel.Rating().Version,
	}
}

// ConvertTo восстанавливает сущность и рейтинг из одного снимка.
func (p Hotel) ConvertTo() *entity.Hotel {
	hotel := entity.With(uuid.FromStringOrNil(p.ID),
		entity.WithName(p.Name),
		entity.WithDescription(p.Description),
		entity.WithStars(p.Stars),
		entity.WithCountry(p.Country),
		entity.WithCity(p.City),
		entity.WithAddress(p.Address),
		entity.WithLatitude(p.Latitude),
		entity.WithLongitude(p.Longitude),
	)

	hotel.SetRating(dto.Rating{
		Value:       p.Rating,
		ReviewCount: p.ReviewCount,
		Version:     p.RatingVersion,
	})

	return hotel
}
