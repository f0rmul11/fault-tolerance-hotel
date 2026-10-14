package httptransport

import (
	"context"
	"net/http"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

// Options настраивает generated HTTP-адаптер Clay; тело ошибки сериализует grpc-gateway.
func Options() []runtime.ServeMuxOption {
	return []runtime.ServeMuxOption{
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{
				UseProtoNames:   true,
				EmitUnpopulated: true,
			},
		}),
		runtime.WithIncomingHeaderMatcher(func(key string) (string, bool) {
			switch strings.ToLower(key) {
			case "x-request-id", "x-demo-role", "x-demo-actor":
				return strings.ToLower(key), true
			default:
				return runtime.DefaultHeaderMatcher(key)
			}
		}),
		runtime.WithOutgoingHeaderMatcher(func(key string) (string, bool) {
			return key, strings.HasPrefix(key, "x-")
		}),
		runtime.WithErrorHandler(errorHandler),
		runtime.WithRoutingErrorHandler(func(ctx context.Context, mux *runtime.ServeMux, marshaler runtime.Marshaler, w http.ResponseWriter, r *http.Request, code int) {
			err := status.Error(codes.NotFound, "Маршрут не найден")
			if code == http.StatusMethodNotAllowed {
				err = status.Error(codes.Unimplemented, "Метод запроса не поддерживается")
			}
			runtime.DefaultHTTPErrorHandler(ctx, mux, marshaler, w, r, &runtime.HTTPStatusError{
				HTTPStatus: code,
				Err:        err,
			})
		}),
	}
}

func errorHandler(ctx context.Context, mux *runtime.ServeMux, marshaler runtime.Marshaler, w http.ResponseWriter, r *http.Request, err error) {
	code := runtime.HTTPStatusFromCode(status.Code(err))
	switch status.Code(err) {
	case codes.InvalidArgument:
		code = http.StatusUnprocessableEntity
		md, _ := runtime.ServerMetadataFromContext(ctx)
		if len(md.TrailerMD.Get("x-error-code")) == 0 {
			err = status.Error(codes.InvalidArgument, "Некорректные параметры запроса")
		}
	case codes.FailedPrecondition, codes.Aborted:
		code = http.StatusConflict
	}
	runtime.DefaultHTTPErrorHandler(ctx, mux, marshaler, w, r, &runtime.HTTPStatusError{
		HTTPStatus: code,
		Err:        err,
	})
}
