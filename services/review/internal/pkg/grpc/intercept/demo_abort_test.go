package intercept

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "review/internal/pkg/pb/review/review/v1"
)

func TestHandlerUnavailable(t *testing.T) {
	for _, method := range []string{pb.ReviewService_GetHotelRatings_FullMethodName, "/grpc.health.v1.Health/Check"} {
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-hotelflow-handler-unavailable", "1"))
		called := false
		_, err := DemoDelayInterceptor()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, func(context.Context, any) (any, error) {
			called = true
			return nil, nil
		})
		if method == pb.ReviewService_GetHotelRatings_FullMethodName {
			if called || status.Code(err) != codes.Unavailable {
				t.Fatalf("called=%v err=%v", called, err)
			}
		} else if !called || err != nil {
			t.Fatalf("probe affected: %v", err)
		}
	}
}
