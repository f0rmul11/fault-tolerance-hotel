package intercept

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// ClientName задаёт identity вызывающего сервиса, не копируя внешний HTTP header.
// В учебном стенде это доверенная metadata, а не замена mTLS-аутентификации.
func ClientName(name string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, conn *grpc.ClientConn, invoke grpc.UnaryInvoker, options ...grpc.CallOption) error {
		md, _ := metadata.FromOutgoingContext(ctx)
		md = md.Copy()
		md.Set("x-client-name", name)
		return invoke(metadata.NewOutgoingContext(ctx, md), method, req, reply, conn, options...)
	}
}
