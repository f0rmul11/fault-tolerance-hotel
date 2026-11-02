package circuit

import (
	"context"
	"errors"
	"log/slog"

	"github.com/sony/gobreaker/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// UnaryClientInterceptor унарный клиентский перехватчик
func (b *Breaker) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if err := ctx.Err(); err != nil {
			return status.FromContextError(err).Err()
		}
		state, ok := b.getBreakerByMethod(method)
		if !ok || state == nil {
			slog.Debug("no breaker found for method or disabled", "method", method)
			return invoker(ctx, method, req, reply, cc, opts...)
		}

		_, err := state.breaker.Execute(func() (interface{}, error) {
			callErr := invoker(ctx, method, req, reply, cc, opts...)
			// Истёкший deadline учитываем как сбой, отмену вызывающей стороны — нет.
			if ctx.Err() != nil {
				callErr = status.FromContextError(ctx.Err()).Err()
			}
			return nil, callErr
		})
		if err != nil {
			// Если это ошибка circuit breaker, возвращаем ее
			if errors.Is(err, gobreaker.ErrOpenState) {
				return ErrCircuitIsOpen
			}

			if errors.Is(err, gobreaker.ErrTooManyRequests) {
				return ErrTooManyRequests
			}

			return err
		}
		return nil
	}
}
