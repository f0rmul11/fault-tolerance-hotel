package gateway

import (
	"google.golang.org/grpc"

	"booking/config"
	"booking/internal/infrastructure/gateway/hotels"
)

type Registry struct {
	Hotels *hotels.Client
}

func NewRegistry(conn map[string]grpc.ClientConnInterface) *Registry {
	return &Registry{
		Hotels: hotels.NewClient(hotels.NewExternalClient(conn[config.HotelsService])),
	}
}
