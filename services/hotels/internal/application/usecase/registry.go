package usecase

import (
	"hotels/internal/application/service"
	hotelget "hotels/internal/application/usecase/hotel/get"
	hotellist "hotels/internal/application/usecase/hotel/list"
	"hotels/internal/application/usecase/hotel/refresh"
	roomget "hotels/internal/application/usecase/room/get"
	roomlist "hotels/internal/application/usecase/room/list"
	runtimeconfig "hotels/internal/config"
	"hotels/internal/infrastructure/adapter"
	"hotels/internal/infrastructure/storage"
)

type Registry struct {
	RefreshHotel *refresh.UseCase
	ListHotels   *hotellist.UseCase
	GetHotel     *hotelget.UseCase
	GetRoom      *roomget.UseCase
	ListRooms    *roomlist.UseCase
}

func NewRegistry(
	stores *storage.Registry,
	services *service.Registry,
	adapters *adapter.Registry,
	rtConfig *runtimeconfig.Client,
) *Registry {
	return &Registry{
		RefreshHotel: refresh.New(adapters.Hotels),
		ListHotels:   hotellist.New(stores.Hotels, services.HotelEnrich),
		GetHotel:     hotelget.New(adapters.Hotels),
		GetRoom:      roomget.New(stores.Rooms),
		ListRooms:    roomlist.New(stores.Rooms, rtConfig),
	}
}
