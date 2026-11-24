package adapter

import (
	"github.com/valkey-io/valkey-go"

	"hotels/config"
	"hotels/internal/application/service"
	"hotels/internal/infrastructure/adapter/hotel"
	"hotels/internal/infrastructure/adapter/hotel/payload"
	"hotels/internal/infrastructure/storage"
	"hotels/internal/pkg/cache"
)

type Registry struct {
	Hotels *hotel.Adapter
}

func NewRegistry(client valkey.Client, stores *storage.Registry, services *service.Registry) *Registry {
	return &Registry{
		Hotels: hotel.NewAdapter(
			cache.NewClient[payload.Hotel, *payload.Hotel](client),
			stores.Hotels,
			services.HotelRatings,
			config.Instance().Cache,
		),
	}
}
