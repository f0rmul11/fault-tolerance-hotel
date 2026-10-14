package gateway

import (
	"google.golang.org/grpc"

	"hotels/config"
	"hotels/internal/infrastructure/gateway/review"
)

type Registry struct {
	Review *review.Client
}

func NewRegistry(conn map[string]grpc.ClientConnInterface) *Registry {
	return &Registry{Review: review.NewClient(review.NewExternalClient(conn[config.ReviewService]))}
}
