package retry

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Protect the previous lesson's opt-in retry and pushback behavior.
func TestRetryDemoPolicies(t *testing.T) {
	for _, test := range []struct {
		name     string
		enabled  bool
		pushback string
		calls    int
		elapsed  time.Duration
	}{
		{name: "disabled", calls: 1},
		{name: "server delay", enabled: true, pushback: "1500", calls: 3, elapsed: 3 * time.Second},
		{name: "server rejects retry", enabled: true, pushback: "-1", calls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				enabled := test.enabled
				interceptor := NewRetry(Config{Default: DefaultConfig{MainConfig: MainConfig{Enabled: &enabled, MaxAttempts: 3}}}).UnaryClientInterceptor()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				start := time.Now()
				calls := 0
				err := interceptor(ctx, "/booking.booking.v1.BookingService/CreateBooking", nil, nil, nil,
					func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, options ...grpc.CallOption) error {
						calls++
						for _, option := range options {
							if trailer, ok := option.(grpc.TrailerCallOption); ok && test.pushback != "" {
								*trailer.TrailerAddr = metadata.Pairs("grpc-retry-pushback-ms", test.pushback)
							}
						}
						return status.Error(codes.Unavailable, "demo fault")
					})
				if status.Code(err) != codes.Unavailable || calls != test.calls || time.Since(start) != test.elapsed {
					t.Fatalf("calls=%d elapsed=%s error=%v", calls, time.Since(start), err)
				}
			})
		})
	}
}
