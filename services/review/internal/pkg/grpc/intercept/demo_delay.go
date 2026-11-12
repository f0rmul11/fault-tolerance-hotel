package intercept

import (
	"context"
	"strconv"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "review/internal/pkg/pb/review/review/v1"
)

const (
	handlerDelayHeader             = "x-hotelflow-handler-delay-ms"
	handlerDelayPerInflightHeader  = "x-hotelflow-handler-delay-per-inflight-ms"
	handlerMaxDelayHeader          = "x-hotelflow-handler-max-delay-ms"
	handlerInflightThresholdHeader = "x-hotelflow-handler-inflight-threshold"
)

// DemoDelayInterceptor эмулирует задержку или отказ внутри server span Review.
// Параметр передаёт Envoy по команде hotelctl; Echo и probes не задерживаются.
func DemoDelayInterceptor() grpc.UnaryServerInterceptor {
	// One counter per server interceptor, after load-shedding admission.
	// Count the whole call, including delay and DB work, even without injection.
	var inFlight atomic.Int64

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		if info.FullMethod != pb.ReviewService_GetHotelRatings_FullMethodName {
			return next(ctx, req)
		}
		active := inFlight.Add(1)
		defer inFlight.Add(-1)

		// Отказ происходит внутри Review: server span и метрики учитывают запрос.
		if unavailable := metadata.ValueFromIncomingContext(ctx, "x-hotelflow-handler-unavailable"); len(unavailable) == 1 && unavailable[0] == "1" {
			trace.SpanFromContext(ctx).AddEvent("demo.handler.unavailable")
			return nil, status.Error(codes.Unavailable, "Review временно недоступен: учебный сбой")
		}
		delay := handlerDelay(ctx, active)
		if delay == 0 {
			return next(ctx, req)
		}

		span := trace.SpanFromContext(ctx)
		span.SetAttributes(
			attribute.Int64("demo.handler_delay_ms", delay.Milliseconds()),
			attribute.Int64("demo.handler_inflight", active),
		)
		span.AddEvent("demo.delay.started")
		timer := time.NewTimer(delay)
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

// The delay is fixed at admission, not recalculated while the call waits.
// All durations in metadata are integer milliseconds, bounded to one minute.
func handlerDelay(ctx context.Context, inFlight int64) time.Duration {
	read := func(key string) int64 {
		values := metadata.ValueFromIncomingContext(ctx, key)
		if len(values) != 1 {
			return 0
		}
		value, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || value < 1 || value > 60000 {
			return 0
		}
		return value
	}
	base := read(handlerDelayHeader)
	if base == 0 {
		return 0
	}
	if len(metadata.ValueFromIncomingContext(ctx, handlerDelayPerInflightHeader)) == 0 &&
		len(metadata.ValueFromIncomingContext(ctx, handlerMaxDelayHeader)) == 0 {
		return time.Duration(base) * time.Millisecond
	}
	step := read(handlerDelayPerInflightHeader)
	maximum := read(handlerMaxDelayHeader)
	if step == 0 || maximum < base || inFlight < 1 {
		return 0
	}
	threshold := int64(1)
	if values := metadata.ValueFromIncomingContext(ctx, handlerInflightThresholdHeader); len(values) > 0 {
		if len(values) != 1 {
			return 0
		}
		value, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || value < 1 || value > 1_000_000 {
			return 0
		}
		threshold = value
	}
	excess := max(0, inFlight-threshold)
	// Cap before multiplying, so even a very large count cannot overflow.
	if excess > (maximum-base)/step {
		return time.Duration(maximum) * time.Millisecond
	}
	return time.Duration(base+excess*step) * time.Millisecond
}
