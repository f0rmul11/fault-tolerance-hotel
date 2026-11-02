package intercept

import (
	"context"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "review/internal/pkg/pb/review/review/v1"
)

const handlerDelayHeader = "x-hotelflow-handler-delay-ms"

// DemoDelayInterceptor эмулирует медленную обработку внутри server span Review.
// Параметр передаёт Envoy по команде hotelctl; Echo и probes не задерживаются.
func DemoDelayInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		if info.FullMethod != pb.ReviewService_GetHotelRatings_FullMethodName {
			return next(ctx, req)
		}
		values := metadata.ValueFromIncomingContext(ctx, handlerDelayHeader)
		if len(values) != 1 {
			return next(ctx, req)
		}
		milliseconds, err := strconv.Atoi(values[0])
		if err != nil || milliseconds < 1 || milliseconds > 60000 {
			return next(ctx, req)
		}

		span := trace.SpanFromContext(ctx)
		span.SetAttributes(attribute.Int("demo.handler_delay_ms", milliseconds))
		span.AddEvent("demo.delay.started")
		timer := time.NewTimer(time.Duration(milliseconds) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			span.AddEvent("demo.delay.canceled", trace.WithAttributes(attribute.String("reason", ctx.Err().Error())))
			return nil, status.FromContextError(ctx.Err()).Err()
		case <-timer.C:
			span.AddEvent("demo.delay.finished")
		}
		return next(ctx, req)
	}
}
