package ratelimit

import (
	"context"

	"google.golang.org/grpc"
)

func (l *Limiter) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		var response any
		err := l.doIfAllowed(ctx, info.FullMethod, func() error {
			var err error
			response, err = handler(ctx, req)
			return err
		})
		return response, err
	}
}
