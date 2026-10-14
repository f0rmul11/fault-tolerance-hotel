package intercept

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	app "booking/internal/pkg/bookingerror"
)

func TestErrorMapping(t *testing.T) {
	for _, tt := range []struct {
		name string
		code app.Code
		want codes.Code
	}{
		{
			"invalid",
			app.InvalidArgument,
			codes.InvalidArgument,
		},
		{
			"not found",
			app.NotFound,
			codes.NotFound,
		},
		{
			"cas",
			app.Conflict,
			codes.Aborted,
		},
		{
			"business",
			app.FailedPrecondition,
			codes.FailedPrecondition,
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
			"timeout",
			app.DeadlineExceeded,
			codes.DeadlineExceeded,
		},
		{
			"internal",
			app.Internal,
			codes.Internal,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := app.New(
				app.WithCode(int(tt.code)),
				app.WithMessage("Ошибка операции"),
				app.WithClassification("internal"),
				app.WithBase((errors.New("database password secret")).Error()),
			)
			err := ToStatus(context.Background(), fmt.Errorf("wrapped: %w", e))
			if status.Code(err) != tt.want {
				t.Fatalf("code=%v want=%v", status.Code(err), tt.want)
			}
			if status.Convert(err).Message() != "Ошибка операции" {
				t.Fatal(err)
			}
		})
	}
}

func TestUnknownErrorIsSafe(t *testing.T) {
	err := ToStatus(context.Background(), errors.New("database password secret"))
	if strings.Contains(err.Error(), "secret") || status.Code(err) != codes.Internal {
		t.Fatal(err)
	}
}

func TestRPCErrorCode(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status codes.Code
		reason string
		want   app.Code
	}{
		{
			name:   "known rejection",
			status: codes.FailedPrecondition,
			reason: "INVALID_STATE",
			want:   app.FailedPrecondition,
		},
		{
			name:   "wrong status",
			status: codes.Internal,
			reason: "INVALID_STATE",
			want:   app.Internal,
		},
		{
			name:   "unknown reason",
			status: codes.FailedPrecondition,
			reason: "UNKNOWN",
			want:   app.FailedPrecondition,
		},
		{
			name:   "missing metadata",
			status: codes.Unavailable,
			want:   app.Unavailable,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			base := status.Error(tt.status, "private dependency details")
			got := FromRPC(base, metadata.Pairs("x-error-code", tt.reason))
			if got.Code() != tt.want || got.Classification() != "gRPC" || got.BaseMessage() != base.Error() {
				t.Fatalf("code=%v error=%v", got.Code(), got)
			}
			if strings.Contains(got.Message(), "private") {
				t.Fatalf("unsafe message: %s", got.Message())
			}
		})
	}
}

func TestContextErrors(t *testing.T) {
	for _, tt := range []struct {
		err  error
		want codes.Code
	}{
		{
			context.Canceled,
			codes.Canceled,
		},
		{
			context.DeadlineExceeded,
			codes.DeadlineExceeded,
		},
	} {
		if got := status.Code(ToStatus(context.Background(), tt.err)); got != tt.want {
			t.Fatalf("%v", got)
		}
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
