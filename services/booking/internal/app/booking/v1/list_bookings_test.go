package booking

import (
	"booking/internal/application/usecase"
	"booking/internal/application/usecase/booking/list"
	"booking/internal/domain/entity"
	"booking/internal/domain/vo"
	"booking/internal/pkg/grpc/intercept"
	pb "booking/internal/pkg/pb/booking/booking/v1"
	"context"
	"testing"

	app "booking/internal/pkg/bookingerror"

	"github.com/gofrs/uuid"
	"google.golang.org/grpc"
)

type listStorage struct {
	guest uuid.UUID
	calls int
}

func (f *listStorage) List(_ context.Context, guest uuid.UUID) (entity.BookingList, error) {
	f.guest = guest
	f.calls++
	return entity.BookingList{
		Version: vo.Version{Old: 12},
	}, nil
}

func TestListIdentityAndEmptyVersion(t *testing.T) {
	guest := uuid.Must(uuid.NewV4()).String()
	storage := &listStorage{}
	server := New(&usecase.Registry{ListBookings: list.New(storage)})
	request := &pb.ListBookingsRequest{Context: &pb.RequestContext{
		DemoRole:  pb.RequestContext_GUEST.Enum(),
		DemoActor: &guest,
		RequestId: uuid.Must(uuid.NewV4()).String(),
	}}
	invoke := func() (any, error) {
		return intercept.AuthInterceptor()(t.Context(), request, &grpc.UnaryServerInfo{FullMethod: pb.BookingService_ListBookings_FullMethodName}, func(ctx context.Context, req any) (any, error) {
			return server.ListBookings(ctx, req.(*pb.ListBookingsRequest))
		})
	}
	out, err := invoke()
	if err != nil {
		t.Fatal(err)
	}
	response := out.(*pb.ListBookingsResponse)
	if response.BookingsVersion != 12 || len(response.Bookings) != 0 || storage.guest.String() != guest {
		t.Fatal("list lost identity or empty-list version")
	}
	request.Context.DemoRole = pb.RequestContext_RECEPTION.Enum()
	if _, err := invoke(); !app.IsCode(err, app.PermissionDenied) || storage.calls != 1 {
		t.Fatalf("list must be guest-only: %v", err)
	}
}
