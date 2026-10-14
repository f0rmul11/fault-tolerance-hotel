package booking

import (
	"context"
	"errors"
	"testing"

	"github.com/gofrs/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"booking/internal/application/usecase"
	"booking/internal/application/usecase/booking/create"
	"booking/internal/domain/entity"
	"booking/internal/domain/vo"
	bookingdto "booking/internal/dto/booking"
	roomdto "booking/internal/dto/room"
	"booking/internal/pkg/bookingerror"
	"booking/internal/pkg/grpc/intercept"
	pb "booking/internal/pkg/pb/booking/booking/v1"
)

type creatorFake struct {
	calls   int
	request create.Input
	err     error
}

func (f *creatorFake) FindByKey(context.Context, uuid.UUID, uuid.UUID) (*entity.Booking, error) {
	return nil, nil
}

func (f *creatorFake) Get(context.Context, int64) (roomdto.Room, error) {
	return roomdto.Room{ID: 1, Number: "101", RoomType: "Standard"}, nil
}

func (f *creatorFake) Confirm(_ context.Context, b *entity.Booking, key uuid.UUID, _ vo.Limits) (*entity.Booking, error) {
	f.calls++
	f.request = create.Input{
		Context: bookingdto.Context{
			ActorID:   b.GuestID(),
			RequestID: b.RequestID(),
		},
		RoomID:         b.RoomID(),
		Period:         b.Period(),
		IdempotencyKey: key,
	}
	return b, f.err
}

func testServer(fake *creatorFake) *Server {
	return New(&usecase.Registry{CreateBooking: create.New(fake, fake, fake)})
}

func TestCreateMapping(t *testing.T) {
	fake := &creatorFake{}
	server := testServer(fake)
	actor := "de3dab97-8166-4908-97ab-ce3e20f7df75"
	r := &pb.CreateBookingRequest{
		Context: &pb.RequestContext{
			RequestId: "ba704156-cfb8-41cd-899f-76021a2df91d",
			DemoRole:  pb.RequestContext_GUEST.Enum(),
			DemoActor: &actor,
		},
		RoomId:         "1",
		IdempotencyKey: "e886b633-1e19-45fd-b06d-c2a17e61ccbf",
		StartsAt:       "2026-10-12T00:00:00Z",
		EndsAt:         "2026-10-13T00:00:00Z",
	}

	out, err := invokeCreate(server, r)
	if err != nil {
		t.Fatal(err)
	}

	if out.Booking.Status != pb.BookingView_CONFIRMED ||
		out.Booking.Version != 1 || out.Booking.RoomId != "1" || out.Booking.RoomNumber != "101" || out.Booking.RoomType != "Standard" {
		t.Fatalf("wrong response: %+v", out.Booking)
	}

	if fake.request.Context.ActorID.String() != actor || fake.request.IdempotencyKey.String() != r.IdempotencyKey {
		t.Fatal("guest missing")
	}
	for _, roomID := range []string{"broken", "", "0", "-1", "9223372036854775808"} {
		t.Run(roomID, func(t *testing.T) {
			r.RoomId = roomID
			_, err = invokeCreate(server, r)
			if !bookingerror.IsCode(err, bookingerror.InvalidArgument) || fake.calls != 1 {
				t.Fatal("invalid room ID reached storage")
			}
		})
	}

}

func invokeCreate(server *Server, request *pb.CreateBookingRequest) (*pb.CreateBookingResponse, error) {
	result, err := intercept.AuthInterceptor()(context.Background(), request, &grpc.UnaryServerInfo{
		FullMethod: pb.BookingService_CreateBooking_FullMethodName,
	}, func(ctx context.Context, req any) (any, error) {
		return server.CreateBooking(ctx, req.(*pb.CreateBookingRequest))
	})
	if err != nil {
		return nil, err
	}

	return result.(*pb.CreateBookingResponse), nil
}

type transportStream struct{ trailers metadata.MD }

func (*transportStream) Method() string {
	return pb.BookingService_CreateBooking_FullMethodName
}

func (*transportStream) SetHeader(metadata.MD) error {
	return nil
}

func (*transportStream) SendHeader(metadata.MD) error {
	return nil
}

func (s *transportStream) SetTrailer(md metadata.MD) error {
	s.trailers = metadata.Join(s.trailers, md)
	return nil
}

func TestFailureDoesNotAdvertiseUncommittedBooking(t *testing.T) {
	server := testServer(&creatorFake{err: errors.New("database connection lost")})
	stream := &transportStream{}
	ctx := grpc.NewContextWithServerTransportStream(context.Background(), stream)
	actor := "de3dab97-8166-4908-97ab-ce3e20f7df75"
	request := &pb.CreateBookingRequest{
		Context: &pb.RequestContext{
			RequestId: "ba704156-cfb8-41cd-899f-76021a2df91d",
			DemoRole:  pb.RequestContext_GUEST.Enum(),
			DemoActor: &actor,
		},
		RoomId:         "1",
		IdempotencyKey: "e886b633-1e19-45fd-b06d-c2a17e61ccbf",
		StartsAt:       "2026-10-12T00:00:00Z",
		EndsAt:         "2026-10-13T00:00:00Z",
	}
	info := &grpc.UnaryServerInfo{FullMethod: pb.BookingService_CreateBooking_FullMethodName}
	response, err := intercept.ErrorInterceptor()(ctx, request, info, func(ctx context.Context, request any) (any, error) {
		return intercept.AuthInterceptor()(ctx, request, info, func(ctx context.Context, request any) (any, error) {
			return server.CreateBooking(ctx, request.(*pb.CreateBookingRequest))
		})
	})
	if response != nil || status.Code(err) != codes.Internal {
		t.Fatalf("response=%v error=%v", response, err)
	}
	ids := stream.trailers.Get("x-booking-id")
	if len(ids) != 0 {
		t.Fatalf("uncommitted booking ID leaked: %v", stream.trailers)
	}
}
