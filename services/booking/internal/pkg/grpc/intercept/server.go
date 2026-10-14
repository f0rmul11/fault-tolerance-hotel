package intercept

import (
	"context"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	app "booking/internal/pkg/bookingerror"
	pb "booking/internal/pkg/pb/booking/booking/v1"
)

// Описание wire-ошибки принадлежит транспорту, не структуре app.Error.
type errorStatus struct {
	code    codes.Code
	reason  string
	message string
}

var errorStatuses = map[app.Code]errorStatus{
	app.External: {
		code:    codes.Internal,
		reason:  "INTERNAL_ERROR",
		message: "Внутренняя ошибка сервиса",
	},
	app.Internal: {
		code:    codes.Internal,
		reason:  "INTERNAL_ERROR",
		message: "Внутренняя ошибка сервиса",
	},
	app.NotFound: {
		code:    codes.NotFound,
		reason:  "NOT_FOUND",
		message: "Запись не найдена",
	},
	app.Conflict: {
		code:    codes.Aborted,
		reason:  "INVALID_STATE",
		message: "Данные изменились, повторите операцию",
	},
	app.FailedPrecondition: {
		code:    codes.FailedPrecondition,
		reason:  "INVALID_STATE",
		message: "Операция недоступна в текущем состоянии",
	},
	app.InvalidArgument: {
		code:    codes.InvalidArgument,
		reason:  "INVALID_REQUEST",
		message: "Некорректные параметры запроса",
	},
	app.PermissionDenied: {
		code:    codes.PermissionDenied,
		reason:  "FORBIDDEN",
		message: "Недостаточно прав для выполнения операции",
	},
	app.Unavailable: {
		code:    codes.Unavailable,
		reason:  "DEPENDENCY_UNAVAILABLE",
		message: "Сервис временно недоступен",
	},
	app.DeadlineExceeded: {
		code:    codes.DeadlineExceeded,
		reason:  "DEPENDENCY_TIMEOUT",
		message: "Время ожидания ответа сервиса истекло",
	},
	app.Canceled: {
		code:    codes.Canceled,
		reason:  "OUTCOME_UNKNOWN",
		message: "Запрос отменён; результат операции может быть неизвестен",
	},
	app.DataIntegrity: {
		code:    codes.Internal,
		reason:  "DATA_INTEGRITY_ERROR",
		message: "Обнаружены некорректные данные",
	},
}

func FromRPC(err error, md metadata.MD) *app.Error {
	if err == nil {
		return nil
	}
	if typed, ok := app.As(err); ok {
		return typed
	}

	rpcCode := status.Code(err)
	if errors.Is(err, context.Canceled) {
		rpcCode = codes.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		rpcCode = codes.DeadlineExceeded
	}

	code := app.Internal
	switch rpcCode {
	case codes.NotFound:
		code = app.NotFound
	case codes.InvalidArgument:
		code = app.InvalidArgument
	case codes.PermissionDenied:
		code = app.PermissionDenied
	case codes.Aborted:
		code = app.Conflict
	case codes.FailedPrecondition:
		code = app.FailedPrecondition
	case codes.Unavailable:
		code = app.Unavailable
	case codes.DeadlineExceeded:
		code = app.DeadlineExceeded
	case codes.Canceled:
		code = app.Canceled
	}

	// Уточняющий код принимается только вместе с соответствующим gRPC-статусом.
	if reasons := md.Get("x-error-code"); len(reasons) > 0 {
		for candidate, description := range errorStatuses {
			if candidate > app.Canceled && description.reason == reasons[0] && description.code == rpcCode {
				code = candidate
				break
			}
		}
	}

	return app.New(
		app.WithCode(int(code)),
		app.WithClassification("gRPC"),
		app.WithMessage(errorStatuses[code].message),
		app.WithBase(err.Error()),
	)
}

func ToStatus(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	typed := FromRPC(err, nil)
	description, known := errorStatuses[typed.Code()]
	message := typed.Message()
	if !known || message == "" {
		description = errorStatuses[app.Internal]
		message = description.message
	}

	trailers := metadata.Pairs("x-error-code", description.reason)
	if incoming, ok := metadata.FromIncomingContext(ctx); ok {
		if ids := incoming.Get("x-request-id"); len(ids) > 0 {
			trailers.Set("x-request-id", ids[0])
		}
	}

	_ = grpc.SetTrailer(ctx, trailers)
	return status.Error(description.code, message)
}

func ErrorInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if r, ok := req.(interface {
			GetContext() *pb.RequestContext
		}); ok && r.GetContext() != nil {
			md, _ := metadata.FromIncomingContext(ctx)
			md = md.Copy()
			md.Set("x-request-id", r.GetContext().RequestId)
			ctx = metadata.NewIncomingContext(ctx, md)
		}

		v, err := handler(ctx, req)
		if err != nil {
			return nil, ToStatus(ctx, err)
		}

		return v, nil
	}
}
