package service

import (
	"hotels/internal/application/service/hotel/enrich"
	"hotels/internal/infrastructure/gateway"
)

type Registry struct {
	HotelEnrich *enrich.Service
}

func NewRegistry(gateways *gateway.Registry) *Registry {
	return &Registry{HotelEnrich: enrich.New(gateways.Review)}
}
