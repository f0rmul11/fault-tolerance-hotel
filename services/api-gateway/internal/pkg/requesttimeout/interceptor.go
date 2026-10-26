package requesttimeout

import (
	"context"

	"google.golang.org/grpc"
)

// Interceptor also covers direct unary gRPC calls. An earlier HTTP/client
// deadline is preserved, so entering the handler never replenishes its budget.
func Interceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		ctx, cancel := context.WithTimeout(ctx, MaxDuration)
		defer cancel()

		return next(ctx, request)
	}
}
