package service

import (
	"hotels/internal/application/service/hotel/enrich"
	"hotels/internal/application/service/hotel/ratings"
	"hotels/internal/infrastructure/gateway"
)

type Registry struct {
	HotelRatings *ratings.Service
	HotelEnrich  *enrich.Service
}

func NewRegistry(gateways *gateway.Registry, fallback bool) *Registry {
	ratingsService := ratings.New(gateways.Review, fallback)
	return &Registry{
		HotelRatings: ratingsService,
		HotelEnrich:  enrich.New(ratingsService),
	}
}
