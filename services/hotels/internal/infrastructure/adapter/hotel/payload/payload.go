package payload

import (
	"time"

	"github.com/gofrs/uuid"

	entity "hotels/internal/domain/entity/hotel"
	dto "hotels/internal/dto/hotel"
	"hotels/internal/infrastructure/adapter/hotel/negative"
	app "hotels/internal/pkg/hotelserror"
)

//go:generate go run github.com/mailru/easyjson/easyjson@v0.9.0 -all payload.go

// Hotel — формат полного снимка в кеше. JSON-теги остаются внутри адаптера.
//
//easyjson:json
type Hotel struct {
	Negative *negative.Payload `json:"negative,omitempty"`

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

	CachedAt time.Time `json:"cached_at"`
}

func (p *Hotel) IsNegative() (err error, negative bool) {
	if p.Negative == nil {
		return nil, false
	}
	return app.New(
		app.WithCode(int(p.Negative.Code)),
		app.WithMessage(p.Negative.Message),
		app.WithClassification(p.Negative.Classification),
		app.WithBase("Ошибка каталога прочитана из кеша"),
	), true
}

func ConvertFromError(err *app.Error) Hotel {
	return Hotel{Negative: &negative.Payload{
		Code:           err.Code(),
		Message:        err.Message(),
		Classification: err.Classification(),
	}}
}

func (p Hotel) Expired(ttl time.Duration) bool {
	return time.Since(p.CachedAt) >= ttl
}

// ConvertFrom вызывается после обогащения отеля рейтингом из Review.
func ConvertFrom(hotel *entity.Hotel) Hotel {
	return Hotel{
		CachedAt:      time.Now(),
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
