package circuit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sony/gobreaker/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestExplicitDisable(t *testing.T) {
	b := NewCircuitBreaker(Config{
		Default:  DefaultConfig{MainConfig: MainConfig{Enabled: true}},
		Handlers: []HandlerConfig{{Method: "/demo.Service/Read", MainConfig: MainConfig{Enabled: false}}},
	})
	if _, enabled := b.getBreakerByMethod("/demo.Service/Read"); enabled {
		t.Fatal("explicit handler enabled:false became enabled:true")
	}
}

func TestCanceledProbeDoesNotProveRecovery(t *testing.T) {
	b := NewCircuitBreaker(Config{Default: DefaultConfig{MainConfig: MainConfig{
		Enabled: true, MaxRequests: 1, OpenTimeout: 5 * time.Millisecond,
		Interval: time.Hour, BucketPeriod: time.Minute,
		ThresholdConsecutive: 1, ThresholdPercentage: 100,
		FailureCodes: []string{"Unavailable", "DeadlineExceeded"},
	}}})
	i := b.UnaryClientInterceptor()
	method := "/demo.Service/Read"
	for range 11 {
		_ = i(context.Background(), method, nil, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			return status.Error(codes.Unavailable, "down")
		})
	}
	s, _ := b.getBreakerByMethod(method)
	if got := s.breaker.State(); got != gobreaker.StateOpen {
		t.Fatalf("want open, got %v", got)
	}
	time.Sleep(10 * time.Millisecond)
	if got := s.breaker.State(); got != gobreaker.StateHalfOpen {
		t.Fatalf("want half-open, got %v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := i(ctx, method, nil, nil, nil, func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		return status.FromContextError(ctx.Err()).Err()
	})
	if status.Code(err) != codes.Canceled {
		t.Fatalf("want canceled, got %v", err)
	}
	if got := s.breaker.State(); got == gobreaker.StateClosed {
		t.Fatal("a canceled RPC closed the circuit without a successful recovery probe")
	}
}

func TestRawDeadlineClassification(t *testing.T) {
	cfg := defaultConfig(MainConfig{})
	if !triggerOnError(context.DeadlineExceeded, cfg.failureCodeSet()) {
		t.Fatal("raw context.DeadlineExceeded is classified as success despite DeadlineExceeded failure code")
	}
}

func TestUnknownFailureCode(t *testing.T) {
	cfg := defaultConfig(MainConfig{FailureCodes: []string{"UNAVAILABLE"}})
	if !triggerOnError(status.Error(codes.Unavailable, "down"), cfg.failureCodeSet()) {
		t.Fatal("unsupported failure code spelling silently disables failure detection")
	}
}

func TestCurrentMinimumSample(t *testing.T) {
	b := NewCircuitBreaker(Config{Default: DefaultConfig{MainConfig: MainConfig{
		Enabled: true, ThresholdConsecutive: 2, ThresholdPercentage: 100,
	}}})
	i := b.UnaryClientInterceptor()
	calls := 0
	for n := range 12 {
		err := i(context.Background(), "/demo.Service/Read", nil, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			calls++
			return status.Error(codes.Unavailable, "down")
		})
		if n == 11 && !errors.Is(err, ErrCircuitIsOpen) {
			t.Fatalf("12th call should be blocked, got %v", err)
		}
	}
	if calls != 11 {
		t.Fatalf("want 11 actual attempts before open, got %d", calls)
	}
}

func TestConcurrentMethodCreation(t *testing.T) {
	b := NewCircuitBreaker(Config{Default: DefaultConfig{MainConfig: MainConfig{Enabled: true}}})
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			for range 100 {
				_ = b.UnaryClientInterceptor()(context.Background(), "/demo.Service/Read", nil, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error { return nil })
			}
		})
	}
	wg.Wait()
}
