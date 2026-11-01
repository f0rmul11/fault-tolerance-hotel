package retry

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"time"

	"api-gateway/internal/pkg/retry/pushback"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// UnaryClientInterceptor повторяет unary RPC в пределах исходного context.
// На соединении необходим grpc.WithDisableRetry(): два configured retry-слоя
// умножают попытки. Transparent retries транспорта эта опция не выключает.
func (r *Retry) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if err := ctx.Err(); err != nil {
			return status.FromContextError(err).Err()
		}

		state, enabled := r.getByMethod(method)
		if !enabled {
			return invoker(ctx, method, req, reply, cc, opts...)
		}

		var (
			lastErr               error
			attemptsSincePushBack int
		)

		for attempt := 0; attempt < state.cfg.MaxAttempts; attempt++ {
			if err := ctx.Err(); err != nil {
				return status.FromContextError(err).Err()
			}

			// FromOutgoingContext возвращает копию; исходная metadata не меняется.
			md, _ := metadata.FromOutgoingContext(ctx)
			if md == nil {
				md = metadata.MD{}
			}
			md.Set("x-retry-attempt", fmt.Sprintf("%d", attempt))
			md.Set("x-retry-max-attempts", fmt.Sprintf("%d", state.cfg.MaxAttempts))
			ctxWithMD := metadata.NewOutgoingContext(ctx, md)

			var headers, trailers metadata.MD
			// Не изменяем backing array caller options: он может быть общим.
			callOpts := make([]grpc.CallOption, len(opts), len(opts)+2)
			copy(callOpts, opts)
			callOpts = append(callOpts, grpc.Header(&headers), grpc.Trailer(&trailers))

			// Лог именно начавшейся попытки, после проверок context и бюджета.
			slog.DebugContext(ctx, "retry: invoke", "method", method, "attempt", attempt+1)
			lastErr = invoker(ctxWithMD, method, req, reply, cc, callOpts...)
			if lastErr == nil {
				state.throttler.SuccessfulRPC()
				return nil
			}

			// В grpc-go обычные response headers (включая content-type)
			// отличаются от trailers-only и означают commitment.
			// Это не предоставляет всех внутренних transport-сигналов native retry.
			if len(headers) != 0 {
				slog.DebugContext(ctx, "retry: stop", "method", method, "reason", "response_headers")
				return lastErr
			}

			pb := pushback.Parse(trailers)
			if pb.Reject {
				// Запрещающий/невалидный pushback учитывается даже при неретрайном коде.
				state.throttler.Throttle()
				slog.DebugContext(ctx, "retry: stop", "method", method, "reason", "pushback")
				return lastErr
			}
			if !isRetryableCode(lastErr, state.cfg.RetryableStatusCodes, state.retryableCodes) {
				return lastErr
			}

			// Списываем отказ сразу, включая последнюю попытку. Это не оплата retry.
			if state.throttler.Throttle() {
				slog.DebugContext(ctx, "retry: stop", "method", method, "reason", "budget")
				return lastErr
			}
			if attempt == state.cfg.MaxAttempts-1 {
				slog.DebugContext(ctx, "retry: stop", "method", method, "reason", "max_attempts")
				return lastErr
			}

			var delay time.Duration
			if pb.Has {
				delay = pb.Delay
				attemptsSincePushBack = 0
			} else {
				delay = r.backoff(attemptsSincePushBack, state.cfg)
				attemptsSincePushBack++
			}
			slog.DebugContext(ctx, "retry: wait", "method", method, "delay", delay, "pushback", pb.Has)
			if err := wait(ctx, delay); err != nil {
				return err
			}
		}
		return lastErr
	}
}

func (r *Retry) backoff(attemptsSincePushback int, cfg MainConfig) time.Duration {
	fact := math.Pow(cfg.BackoffMultiplier, float64(attemptsSincePushback))
	cur := min(float64(cfg.InitialBackoff)*fact, float64(cfg.MaxBackoff))
	cur *= 0.8 + 0.4*rand.Float64()
	return time.Duration(cur)
}

func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return status.FromContextError(ctx.Err()).Err()
	case <-timer.C:
		return nil
	}
}
