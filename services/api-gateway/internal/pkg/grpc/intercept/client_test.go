package intercept

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestProxyPreservesContextAndOwnerErrors(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Minute))
	defer cancel()

	ownerError := status.Error(
		codes.InvalidArgument,
		"Длительность бронирования не разрешена для номера",
	)
	err := ProxyMetadata()(ctx, "/booking/CreateBooking", nil, nil, nil,
		func(actual context.Context, _ string, _, _ any, _ *grpc.ClientConn, opts ...grpc.CallOption) error {
			if actual != ctx {
				t.Fatal("proxy replaced the incoming context")
			}
			for _, opt := range opts {
				if trailer, ok := opt.(grpc.TrailerCallOption); ok {
					*trailer.TrailerAddr = metadata.Pairs("x-error-code", "DURATION_INVALID")
				}
			}
			return ownerError
		},
	)
	if err != ownerError {
		t.Fatalf("owner error was rewritten: %v", err)
	}
}

func TestTransportFailuresHaveRussianMessages(t *testing.T) {
	for _, test := range []struct {
		code    codes.Code
		message string
	}{
		{
			codes.Unavailable,
			"Сервис временно недоступен",
		},
		{
			codes.DeadlineExceeded,
			"Время ожидания ответа сервиса истекло",
		},
		{
			codes.Canceled,
			"Запрос отменён; результат операции может быть неизвестен",
		},
	} {
		t.Run(test.code.String(), func(t *testing.T) {
			err := ProxyMetadata()(context.Background(), "/booking/CreateBooking", nil, nil, nil,
				func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
					return status.Error(test.code, "transport details: private host")
				},
			)
			if status.Code(err) != test.code || status.Convert(err).Message() != test.message {
				t.Fatalf("transport error = %v", err)
			}
		})
	}
}
