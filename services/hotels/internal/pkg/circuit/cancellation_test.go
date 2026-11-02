package circuit

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sony/gobreaker/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCancellationDoesNotDiluteFailurePercentage(t *testing.T) {
	b := NewCircuitBreaker(Config{Default: DefaultConfig{MainConfig: MainConfig{
		Enabled:              true,
		ThresholdConsecutive: 100,
		ThresholdPercentage:  25,
	}}})
	interceptor := b.UnaryClientInterceptor()
	method := "/test.Service/Read"
	for range 100 {
		ctx, cancel := context.WithCancel(t.Context())
		_ = interceptor(ctx, method, nil, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			cancel()
			return status.Error(codes.Canceled, "caller left")
		})
		cancel()
	}
	for range 11 {
		_ = interceptor(t.Context(), method, nil, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			return status.Error(codes.Unavailable, "dependency failed")
		})
	}
	state, _ := b.getBreakerByMethod(method)
	if state.breaker.State() != gobreaker.StateOpen {
		t.Fatal("excluded cancellations diluted the failure ratio")
	}
}

func TestCanceledHalfOpenCallReleasesProbeWithoutClosing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := NewCircuitBreaker(Config{Default: DefaultConfig{MainConfig: MainConfig{
			Enabled:     true,
			MaxRequests: 1,
			OpenTimeout: time.Second,
		}}})
		interceptor := b.UnaryClientInterceptor()
		method := "/test.Service/Read"
		for range 11 {
			_ = interceptor(t.Context(), method, nil, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
				return status.Error(codes.Unavailable, "dependency failed")
			})
		}
		time.Sleep(2 * time.Second)
		ctx, cancel := context.WithCancel(t.Context())
		err := interceptor(ctx, method, nil, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			cancel()
			return status.Error(codes.Canceled, "caller left during probe")
		})
		state, _ := b.getBreakerByMethod(method)
		if status.Code(err) != codes.Canceled || state.breaker.State() != gobreaker.StateHalfOpen {
			t.Fatalf("error=%v state=%v", err, state.breaker.State())
		}
		err = interceptor(t.Context(), method, nil, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			return nil
		})
		if err != nil || state.breaker.State() != gobreaker.StateClosed {
			t.Fatalf("healthy probe: error=%v state=%v", err, state.breaker.State())
		}
	})
}

func TestDeadlineCountsAsFailureAndReopensProbe(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "late_success"},
		{name: "transport_canceled", err: status.Error(codes.Canceled, "stream reset")},
		{name: "transport_unavailable", err: status.Error(codes.Unavailable, "upstream reset")},
		{name: "raw_deadline", err: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				b := NewCircuitBreaker(Config{Default: DefaultConfig{MainConfig: MainConfig{
					Enabled: true, MaxRequests: 1, OpenTimeout: time.Second,
					Interval: time.Hour, BucketPeriod: time.Minute,
					ThresholdConsecutive: 1, ThresholdPercentage: 100,
					FailureCodes: []string{"DeadlineExceeded"},
				}}})
				method := "/test.Service/Read"
				invoke := func() {
					t.Helper()
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
					defer cancel()
					err := b.UnaryClientInterceptor()(ctx, method, nil, nil, nil,
						func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
							<-ctx.Done()
							return test.err
						})
					if status.Code(err) != codes.DeadlineExceeded {
						t.Fatalf("expired RPC returned %v", err)
					}
				}
				invoke()
				state, _ := b.getBreakerByMethod(method)
				if counts := state.breaker.Counts(); counts.TotalFailures != 1 || counts.TotalSuccesses != 0 {
					t.Fatalf("deadline was not counted as failure: %+v", counts)
				}
				for range 10 {
					invoke()
				}
				if state.breaker.State() != gobreaker.StateOpen {
					t.Fatal("repeated deadlines did not open the circuit")
				}
				time.Sleep(2 * time.Second)
				invoke()
				if state.breaker.State() != gobreaker.StateOpen {
					t.Fatal("expired half-open probe did not reopen the circuit")
				}
			})
		})
	}
}
