package loadshedding

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnaryServerInterceptor унарный серверный перехватчик
func (ls *LoadShedding) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		state, ok := ls.getByMethod(info.FullMethod)
		if !ok || state == nil {
			slog.Debug("no load shedding found for method or disabled", "method", info.FullMethod)
			return handler(ctx, req)
		}

		// пробуем захватить токен и выполнить запрос
		token, ok := state.metrics.acquire(ctx, state.queueLimiter)

		// если захватить не удалось (лимит не позволяет)
		if !ok {
			const format = "request dropped by load shedding: server concurrency limit %d exceeded for handler %s"

			return nil, status.Errorf(codes.ResourceExhausted, format, state.limitAlg.EstimatedLimit(), info.FullMethod)
		}

		resp, err := handler(ctx, req)
		// если запрос отменили - нужно отреагировать, чтобы алгоритм пересчитал окно
		if err != nil && ctx.Err() != nil {
			token.OnDropped()
			return resp, err
		}

		// если ошибка, то надо изучить ее составляющую
		if err != nil {
			switch {
			case permanent(err): // если она перманентная
				token.OnIgnore()
			case temporary(err): // если она временная
				token.OnDropped()
			default:
				token.OnSuccess()
			}

			return resp, err
		}

		// если ошибки нет - засчитываем за успех
		token.OnSuccess()
		return resp, err
	}
}
