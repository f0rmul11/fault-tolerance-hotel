package intercept

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	app "hotels/internal/pkg/hotelserror"
)

type transportStream struct {
	trailers metadata.MD
}

func (s *transportStream) Method() string {
	return "/hotels.echo.v1.EchoService/Echo"
}

func (s *transportStream) SetHeader(metadata.MD) error {
	return nil
}

func (s *transportStream) SendHeader(metadata.MD) error {
	return nil
}

func (s *transportStream) SetTrailer(md metadata.MD) error {
	s.trailers = metadata.Join(s.trailers, md)
	return nil
}

func TestErrorInterceptorMapsCategories(t *testing.T) {
	tests := []struct {
		name   string
		code   app.Code
		status codes.Code
	}{
		{
			"internal",
			app.Internal,
			codes.Internal,
		},
		{
			"external",
			app.External,
			codes.Internal,
		},
		{
			"not found",
			app.NotFound,
			codes.NotFound,
		},
		{
			"conflict",
			app.Conflict,
			codes.Aborted,
		},
		{
			"precondition",
			app.FailedPrecondition,
			codes.FailedPrecondition,
		},
		{
			"invalid input",
			app.InvalidArgument,
			codes.InvalidArgument,
		},
		{
			"forbidden",
			app.PermissionDenied,
			codes.PermissionDenied,
		},
		{
			"unavailable",
			app.Unavailable,
			codes.Unavailable,
		},
		{
			"deadline",
			app.DeadlineExceeded,
			codes.DeadlineExceeded,
		},
		{
			"canceled",
			app.Canceled,
			codes.Canceled,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stream := &transportStream{}
			ctx := metadata.NewIncomingContext(
				context.Background(),
				metadata.Pairs("x-request-id", "ba704156-cfb8-41cd-899f-76021a2df91d"),
			)
			ctx = grpc.NewContextWithServerTransportStream(ctx, stream)
			source := app.New(
				app.WithCode(int(test.code)),
				app.WithMessage("Безопасное пояснение"),
				app.WithClassification("internal"),
				app.WithBase((errors.New("secret database details")).Error()),
			)
			wrapped := fmt.Errorf("private wrapper: %w", source)
			result, err := ErrorInterceptor()(
				ctx,
				nil,
				&grpc.UnaryServerInfo{},
				func(context.Context, any) (any, error) {
					return "discarded", wrapped
				},
			)
			if result != nil || status.Code(err) != test.status {
				t.Fatalf("result=%v status=%v", result, status.Code(err))
			}
			message := status.Convert(err).Message()
			if strings.Contains(message, "secret") || strings.Contains(message, "private") || message == "" {
				t.Fatalf("unsafe message: %q", message)
			}
			if stream.trailers.Get("x-error-code")[0] != errorStatuses[test.code].reason ||
				stream.trailers.Get("x-request-id")[0] != "ba704156-cfb8-41cd-899f-76021a2df91d" {
				t.Fatalf("trailers=%v", stream.trailers)
			}
		})
	}
}

func TestErrorNormalizationPreservesMeaningAndHidesCause(t *testing.T) {
	tests := []struct {
		name    string
		input   error
		code    codes.Code
		message string
	}{
		{
			"unknown",
			errors.New("SQL password"),
			codes.Internal,
			"Внутренняя ошибка сервиса",
		},
		{
			"wrapped deadline",
			fmt.Errorf("transport: %w", context.DeadlineExceeded),
			codes.DeadlineExceeded,
			"Время ожидания ответа сервиса истекло",
		},
		{
			"wrapped cancellation",
			fmt.Errorf("transport: %w", context.Canceled),
			codes.Canceled,
			"Запрос отменён; результат операции может быть неизвестен",
		},
		{
			"downstream",
			status.Error(codes.Unavailable, "private address"),
			codes.Unavailable,
			"Сервис временно недоступен",
		},
		{
			"external cause",
			app.NewExternalErr("Внешняя ошибка", status.Error(codes.NotFound, "private id")),
			codes.Internal,
			"Внешняя ошибка",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := ToStatus(context.Background(), test.input)
			if status.Code(actual) != test.code || status.Convert(actual).Message() != test.message {
				t.Fatalf("got %v", actual)
			}
		})
	}
}

func TestErrorInterceptorPassesSuccessfulResponse(t *testing.T) {
	response := &struct {
		Message string
	}{
		Message: "echo",
	}
	actual, err := ErrorInterceptor()(
		context.Background(),
		nil,
		&grpc.UnaryServerInfo{},
		func(context.Context, any) (any, error) {
			return response, nil
		},
	)
	if err != nil || actual != response {
		t.Fatalf("result=%v error=%v", actual, err)
	}
	if FromRPC(nil, nil) != nil || ToStatus(context.Background(), nil) != nil {
		t.Fatal("nil error did not remain nil")
	}
}

func TestInputBusinessAndConflictErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want codes.Code
	}{
		{
			name: "invalid input",
			err:  app.NewInvalidArgumentErr("Некорректный идентификатор"),
			want: codes.InvalidArgument,
		},
		{
			name: "business rule",
			err:  app.NewBusinessErr("Операция недоступна в текущем состоянии"),
			want: codes.FailedPrecondition,
		},
		{
			name: "concurrent update",
			err:  app.NewConflictErr("Данные изменились одновременно"),
			want: codes.Aborted,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ToStatus(context.Background(), test.err)
			if status.Code(err) != test.want || status.Convert(err).Message() != test.err.Error() {
				t.Fatalf("code or Russian message lost: %v", err)
			}
		})
	}
}
