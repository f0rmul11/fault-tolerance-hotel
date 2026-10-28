package usecase

import (
	"hotels/internal/application/service"
	hotelget "hotels/internal/application/usecase/hotel/get"
	hotellist "hotels/internal/application/usecase/hotel/list"
	roomget "hotels/internal/application/usecase/room/get"
	roomlist "hotels/internal/application/usecase/room/list"
	runtimeconfig "hotels/internal/config"
	"hotels/internal/infrastructure/gateway"
	"hotels/internal/infrastructure/storage"
)

type Registry struct {
	ListHotels *hotellist.UseCase
	GetHotel   *hotelget.UseCase
	GetRoom    *roomget.UseCase
	ListRooms  *roomlist.UseCase
}

func NewRegistry(
	stores *storage.Registry,
	services *service.Registry,
	gateways *gateway.Registry,
	rtConfig *runtimeconfig.Client,
) *Registry {
	return &Registry{
		ListHotels: hotellist.New(stores.Hotels, services.HotelEnrich),
		GetHotel:   hotelget.New(stores.Hotels, gateways.Review),
		GetRoom:    roomget.New(stores.Rooms),
		ListRooms:  roomlist.New(stores.Rooms, rtConfig),
	}
}
