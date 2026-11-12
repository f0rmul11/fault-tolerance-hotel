package intercept

import (
	"context"
	"math"
	"testing"
	"testing/synctest"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "review/internal/pkg/pb/review/review/v1"
)

func progressiveContext(ctx context.Context) context.Context {
	return metadata.NewIncomingContext(ctx, metadata.Pairs(
		handlerDelayHeader, "20",
		handlerDelayPerInflightHeader, "20",
		handlerMaxDelayHeader, "8000",
	))
}

func TestInflightDelayCalculation(t *testing.T) {
	for _, tt := range []struct {
		inFlight int64
		want     time.Duration
	}{
		{inFlight: 1, want: 20 * time.Millisecond},
		{inFlight: 40, want: 800 * time.Millisecond},
		{inFlight: 100, want: 2 * time.Second},
		{inFlight: 400, want: 8 * time.Second},
		{inFlight: math.MaxInt64, want: 8 * time.Second},
	} {
		if got := handlerDelay(progressiveContext(t.Context()), tt.inFlight); got != tt.want {
			t.Fatalf("inflight=%d: delay=%v, want %v", tt.inFlight, got, tt.want)
		}
	}
}

func TestInflightDelayRejectsMalformedMetadata(t *testing.T) {
	for _, pairs := range [][]string{
		{handlerDelayHeader, "20", handlerDelayPerInflightHeader, "-1", handlerMaxDelayHeader, "8000"},
		{handlerDelayHeader, "20", handlerDelayPerInflightHeader, "20"},
		{handlerDelayHeader, "20", handlerMaxDelayHeader, "8000"},
		{handlerDelayHeader, "20", handlerDelayPerInflightHeader, "20", handlerMaxDelayHeader, "10"},
		{handlerDelayHeader, "20", handlerDelayPerInflightHeader, "20", handlerMaxDelayHeader, "60001"},
		{handlerDelayHeader, "20", handlerDelayPerInflightHeader, "999999999999999999999", handlerMaxDelayHeader, "8000"},
		{handlerDelayHeader, "20", handlerDelayHeader, "30"},
	} {
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(pairs...))
		if got := handlerDelay(ctx, 40); got != 0 {
			t.Fatalf("malformed metadata injected %v: %v", got, pairs)
		}
	}
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(handlerDelayHeader, "20"))
	if handlerDelay(ctx, 40) != 20*time.Millisecond {
		t.Fatal("fixed delay changed")
	}
}

func TestInflightDelayThresholdAndRampCeiling(t *testing.T) {
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(
		handlerDelayHeader, "100",
		handlerDelayPerInflightHeader, "5",
		handlerMaxDelayHeader, "125",
		handlerInflightThresholdHeader, "20",
	))
	for _, tt := range []struct {
		inFlight int64
		want     time.Duration
	}{
		{inFlight: 19, want: 100 * time.Millisecond},
		{inFlight: 20, want: 100 * time.Millisecond},
		{inFlight: 21, want: 105 * time.Millisecond},
		{inFlight: 30, want: 125 * time.Millisecond},
	} {
		if got := handlerDelay(ctx, tt.inFlight); got != tt.want {
			t.Fatalf("inflight=%d: delay=%v, want %v", tt.inFlight, got, tt.want)
		}
	}
}

func TestInflightIncludesHandlerWorkAndReleases(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		interceptor := DemoDelayInterceptor()
		info := &grpc.UnaryServerInfo{FullMethod: pb.ReviewService_GetHotelRatings_FullMethodName}
		next := func(context.Context, any) (any, error) {
			return nil, nil
		}
		// An ordinary call is still active while its handler runs a second call.
		_, err := interceptor(t.Context(), nil, info, func(context.Context, any) (any, error) {
			start := time.Now()
			_, err := interceptor(progressiveContext(t.Context()), nil, info, next)
			if elapsed := time.Since(start); elapsed != 40*time.Millisecond {
				t.Errorf("two active calls: delay=%v", elapsed)
			}
			return nil, err
		})
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		_, err = interceptor(progressiveContext(t.Context()), nil, info, next)
		if err != nil || time.Since(start) != 20*time.Millisecond {
			t.Fatalf("counter leaked: delay=%v err=%v", time.Since(start), err)
		}
		// Echo is neither delayed nor counted, even with fault metadata.
		start = time.Now()
		_, err = interceptor(progressiveContext(t.Context()), nil, &grpc.UnaryServerInfo{FullMethod: "/review.echo.v1.EchoService/Echo"}, func(context.Context, any) (any, error) {
			return interceptor(progressiveContext(t.Context()), nil, info, next)
		})
		if err != nil || time.Since(start) != 20*time.Millisecond {
			t.Fatalf("Echo affected inflight: delay=%v err=%v", time.Since(start), err)
		}
	})
}

func TestInflightCancellationReleasesCounter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		interceptor := DemoDelayInterceptor()
		info := &grpc.UnaryServerInfo{FullMethod: pb.ReviewService_GetHotelRatings_FullMethodName}
		ctx, cancel := context.WithCancel(progressiveContext(t.Context()))
		done := make(chan error, 1)
		go func() {
			_, err := interceptor(ctx, nil, info, func(context.Context, any) (any, error) {
				t.Error("canceled call reached handler")
				return nil, nil
			})
			done <- err
		}()
		synctest.Wait()
		cancel()
		if err := <-done; status.Code(err) != codes.Canceled {
			t.Fatal(err)
		}
		start := time.Now()
		_, err := interceptor(progressiveContext(t.Context()), nil, info, func(context.Context, any) (any, error) {
			return nil, nil
		})
		if err != nil || time.Since(start) != 20*time.Millisecond {
			t.Fatalf("canceled call still counted: delay=%v err=%v", time.Since(start), err)
		}
	})
}
