package intercept

import (
	"context"
	"slices"

	"check-in/internal/domain/entity"

	callerdto "check-in/internal/dto/checkin"
	app "check-in/internal/pkg/checkinerror"
	pb "check-in/internal/pkg/pb/check-in/checkin/v1"

	"github.com/gofrs/uuid"
	"google.golang.org/grpc"
)

type callerKey struct{}

var methodRoles = map[string][]entity.Role{
	pb.CheckinService_ListArrivals_FullMethodName:    {entity.RoleReception, entity.RoleOperator},
	pb.CheckinService_ActivateBooking_FullMethodName: {entity.RoleReception, entity.RoleOperator},
	pb.CheckinService_CancelBooking_FullMethodName:   {entity.RoleReception, entity.RoleOperator},
}

// Caller доступен после AuthInterceptor; отсутствие middleware — ошибка сборки сервера.
func Caller(ctx context.Context) callerdto.Context {
	return ctx.Value(callerKey{}).(callerdto.Context)
}

// AuthInterceptor проверяет контекст и разрешённые роли по полному имени RPC.
func AuthInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		roles, protected := methodRoles[info.FullMethod]
		if !protected {
			return handler(ctx, req)
		}
		request, ok := req.(interface {
			GetContext() *pb.RequestContext
		})
		if !ok {
			return nil, invalidContext("Не указан контекст запроса")
		}
		caller, err := parseCaller(request.GetContext(), roles)
		if err != nil {
			return nil, err
		}
		return handler(context.WithValue(ctx, callerKey{}, caller), req)
	}
}

func parseCaller(input *pb.RequestContext, roles []entity.Role) (callerdto.Context, error) {
	if input == nil {
		return callerdto.Context{}, invalidContext("Не указан контекст запроса")
	}
	requestID, err := parseContextID(input.GetRequestId(), "запроса")
	if err != nil {
		return callerdto.Context{}, err
	}
	role := entity.Role(input.GetDemoRole().String())
	if !slices.Contains(roles, role) {
		return callerdto.Context{}, app.New(
			app.WithCode(int(app.PermissionDenied)),
			app.WithMessage("Роль не разрешает эту операцию"),
			app.WithClassification("internal"),
		)
	}
	var actorID uuid.UUID
	if input.GetDemoActor() != "" {
		actorID, err = parseContextID(input.GetDemoActor(), "вызывающего")
		if err != nil {
			return callerdto.Context{}, err
		}
	}
	if actorID == uuid.Nil {
		return callerdto.Context{}, invalidContext("Не указан идентификатор оператора")
	}
	return callerdto.Context{
		RequestID: requestID,
		ActorID:   actorID,
		Role:      role,
	}, nil
}

func parseContextID(value, name string) (uuid.UUID, error) {
	id, err := uuid.FromString(value)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, invalidContext("Некорректный идентификатор " + name)
	}
	return id, nil
}

func invalidContext(message string) error {
	return app.NewInvalidArgumentErr(message)
}
