package auth

import (
	"context"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"api-gateway/internal/pkg/requestid"

	bookingV1 "api-gateway/internal/pkg/pb/api-gateway/booking/v1"
	checkinV1 "api-gateway/internal/pkg/pb/api-gateway/checkin/v1"
	externalV1 "api-gateway/internal/pkg/pb/external/booking/booking/v1"
)

type callerKey struct{}

func Caller(ctx context.Context) *externalV1.RequestContext {
	return ctx.Value(callerKey{}).(*externalV1.RequestContext)
}

// Interceptor проверяет разрешённые роли. Identity передаётся владельцу без валидации.
func Interceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		var roles []externalV1.RequestContext_DemoRole
		switch info.FullMethod {
		case bookingV1.BookingService_CreateBooking_FullMethodName:
			roles = []externalV1.RequestContext_DemoRole{externalV1.RequestContext_GUEST}
		case bookingV1.BookingService_GetBooking_FullMethodName,
			bookingV1.BookingService_CheckAvailability_FullMethodName:
			roles = []externalV1.RequestContext_DemoRole{
				externalV1.RequestContext_GUEST,
				externalV1.RequestContext_RECEPTION,
				externalV1.RequestContext_OPERATOR,
			}
		case checkinV1.CheckinService_ListArrivals_FullMethodName, checkinV1.CheckinService_ActivateBooking_FullMethodName, checkinV1.CheckinService_CancelBooking_FullMethodName:
			roles = []externalV1.RequestContext_DemoRole{externalV1.RequestContext_RECEPTION, externalV1.RequestContext_OPERATOR}
		default:
			return next(ctx, request)
		}

		md, _ := metadata.FromIncomingContext(ctx)
		roleValues := md.Get("x-demo-role")
		if len(roleValues) != 1 {
			return nil, status.Error(codes.PermissionDenied, "Роль не разрешает эту операцию")
		}
		role := externalV1.RequestContext_DemoRole(externalV1.RequestContext_DemoRole_value[roleValues[0]])
		if !slices.Contains(roles, role) {
			return nil, status.Error(codes.PermissionDenied, "Роль не разрешает эту операцию")
		}
		caller := &externalV1.RequestContext{
			DemoRole:  &role,
			RequestId: requestid.FromContext(ctx),
		}
		if actors := md.Get("x-demo-actor"); len(actors) > 0 {
			caller.DemoActor = &actors[0]
		}

		return next(context.WithValue(ctx, callerKey{}, caller), request)
	}
}
