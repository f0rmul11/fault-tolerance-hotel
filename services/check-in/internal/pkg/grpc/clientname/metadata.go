package clientname

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const Header = "x-client-name"

type contextKey struct{}

func NewContext(parent context.Context, name string) context.Context {
	return context.WithValue(parent, contextKey{}, name)
}

func FromContext(ctx context.Context) string {
	if name, ok := ctx.Value(contextKey{}).(string); ok && name != "" {
		return name
	}
	return "unknown"
}

// Имя доверено только внутри учебного кластера. Это не аутентификация клиента.
func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		name := "unknown"
		if names := md.Get(Header); len(names) == 1 && names[0] != "" {
			name = names[0]
		}
		return handler(NewContext(ctx, name), req)
	}
}
