package intercept

import (
	"booking/internal/domain/entity"
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "booking/internal/pkg/pb/booking/booking/v1"
)

const authRequestID = "ba704156-cfb8-41cd-899f-76021a2df91d"

const authActorID = "de3dab97-8166-4908-97ab-ce3e20f7df75"

func guestContext() *pb.RequestContext {
	actor := authActorID
	return &pb.RequestContext{
		RequestId: authRequestID,
		DemoRole:  pb.RequestContext_GUEST.Enum(),
		DemoActor: &actor,
	}
}

func TestAuthPassesValidatedCaller(t *testing.T) {
	request := &pb.CreateBookingRequest{
		Context: guestContext(),
	}
	called := false
	_, err := AuthInterceptor()(context.Background(), request, &grpc.UnaryServerInfo{
		FullMethod: pb.BookingService_CreateBooking_FullMethodName,
	}, func(ctx context.Context, _ any) (any, error) {
		called = true
		caller := Caller(ctx)
		if caller.RequestID.String() != authRequestID ||
			caller.ActorID.String() != authActorID ||
			caller.Role != entity.RoleGuest {
			t.Fatalf("caller=%+v", caller)
		}
		return nil, nil
	})
	if err != nil || !called {
		t.Fatalf("called=%v error=%v", called, err)
	}
}

func TestAuthRejectsBeforeHandlerAndErrorInterceptorMapsIt(t *testing.T) {
	tests := []struct {
		name   string
		change func(*pb.CreateBookingRequest)
		want   codes.Code
	}{
		{
			"missing context",
			func(r *pb.CreateBookingRequest) {
				r.Context = nil
			},
			codes.InvalidArgument,
		},
		{
			"invalid request id",
			func(r *pb.CreateBookingRequest) {
				r.Context.RequestId = "broken"
			},
			codes.InvalidArgument,
		},
		{
			"missing guest id",
			func(r *pb.CreateBookingRequest) {
				r.Context.DemoActor = nil
			},
			codes.InvalidArgument,
		},
		{
			"invalid actor",
			func(r *pb.CreateBookingRequest) {
				s := "broken"
				r.Context.DemoActor = &s
			},
			codes.InvalidArgument,
		},
		{
			"missing role",
			func(r *pb.CreateBookingRequest) {
				r.Context.DemoRole = nil
			},
			codes.PermissionDenied,
		},
		{
			"reception cannot create",
			func(r *pb.CreateBookingRequest) {
				r.Context.DemoRole = pb.RequestContext_RECEPTION.Enum()
			},
			codes.PermissionDenied,
		},
		{
			"operator cannot create",
			func(r *pb.CreateBookingRequest) {
				r.Context.DemoRole = pb.RequestContext_OPERATOR.Enum()
			},
			codes.PermissionDenied,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := &pb.CreateBookingRequest{
				Context: guestContext(),
			}
			test.change(request)
			stream := &authStream{}
			ctx := grpc.NewContextWithServerTransportStream(context.Background(), stream)
			info := &grpc.UnaryServerInfo{
				FullMethod: pb.BookingService_CreateBooking_FullMethodName,
			}
			_, err := ErrorInterceptor()(ctx, request, info, func(ctx context.Context, req any) (any, error) {
				return AuthInterceptor()(
					ctx,
					req,
					info,
					func(context.Context, any) (any, error) {
						t.Fatal("invalid caller reached handler")
						return nil, nil
					},
				)
			})
			if status.Code(err) != test.want {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
			if len(stream.trailers.Get("x-error-code")) != 1 {
				t.Fatal("error reason missing")
			}
			requestIDs := stream.trailers.Get("x-request-id")
			if request.Context != nil &&
				request.Context.RequestId == authRequestID &&
				(len(requestIDs) == 0 || requestIDs[0] != authRequestID) {
				t.Fatal("request correlation lost")
			}
		})
	}
}

func TestGetMethodRolePolicy(t *testing.T) {
	for _, role := range []pb.RequestContext_DemoRole{
		pb.RequestContext_GUEST,
		pb.RequestContext_RECEPTION,
		pb.RequestContext_OPERATOR,
	} {
		t.Run(role.String(), func(t *testing.T) {
			caller := guestContext()
			caller.DemoRole = role.Enum()
			if role != pb.RequestContext_GUEST {
				caller.DemoActor = nil
			}
			request := &pb.GetBookingRequest{
				Context: caller,
			}
			called := false
			_, err := AuthInterceptor()(
				context.Background(),
				request,
				&grpc.UnaryServerInfo{
					FullMethod: pb.BookingService_GetBooking_FullMethodName,
				},
				func(context.Context, any) (any, error) {
					called = true
					return nil, nil
				},
			)
			if err != nil || !called {
				t.Fatalf("role=%v error=%v", role, err)
			}
		})
	}
}

func TestEchoDoesNotRequireBookingContext(t *testing.T) {
	called := false
	_, err := AuthInterceptor()(
		context.Background(),
		struct{}{},
		&grpc.UnaryServerInfo{
			FullMethod: "/echo.v1.EchoService/Echo",
		},
		func(context.Context, any) (any, error) {
			called = true
			return nil, nil
		},
	)
	if err != nil || !called {
		t.Fatalf("called=%v error=%v", called, err)
	}
}

type authStream struct {
	trailers metadata.MD
}

func (*authStream) Method() string {
	return ""
}

func (*authStream) SetHeader(metadata.MD) error {
	return nil
}

func (*authStream) SendHeader(metadata.MD) error {
	return nil
}

func (s *authStream) SetTrailer(md metadata.MD) error {
	s.trailers = metadata.Join(s.trailers, md)
	return nil
}
