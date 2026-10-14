package requestid

import (
	"context"

	"github.com/gofrs/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type contextKey struct{}

func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}

// Interceptor задаёт единый request_id для обработки запроса и заголовка ответа.
func Interceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		var requestID string
		if ids := md.Get("x-request-id"); len(ids) > 0 {
			requestID = ids[0]
		} else {
			id, err := uuid.NewV4()
			if err != nil {
				return nil, status.Error(codes.Internal, "Не удалось создать идентификатор запроса")
			}
			requestID = id.String()
		}
		if err := grpc.SetHeader(ctx, metadata.Pairs("x-request-id", requestID)); err != nil {
			return nil, status.Error(codes.Internal, "Не удалось передать идентификатор запроса")
		}
		return next(context.WithValue(ctx, contextKey{}, requestID), request)
	}
}
