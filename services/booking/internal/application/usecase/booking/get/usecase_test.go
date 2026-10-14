package get

import (
	"context"
	"testing"

	"github.com/gofrs/uuid"

	"booking/internal/domain/entity"
	bookingdto "booking/internal/dto/booking"
	"booking/internal/pkg/bookingerror"
)

type storageFake struct {
	b   *entity.Booking
	err error
}

func (s storageFake) Get(context.Context, uuid.UUID) (*entity.Booking, error) {
	return s.b, s.err
}

func TestGuestOwnershipAndStaffAccess(t *testing.T) {
	owner := uuid.FromStringOrNil("de3dab97-8166-4908-97ab-ce3e20f7df75")
	other := uuid.FromStringOrNil("39a654bc-aed7-472d-8cc3-535b826f4ef0")
	id := uuid.FromStringOrNil("cea3dc2b-8059-4cd4-a2cf-574b7b54f802")
	requestID := uuid.FromStringOrNil("ba704156-cfb8-41cd-899f-76021a2df91d")
	s := New(storageFake{
		b: entity.With(id,
			entity.WithGuestID(owner)),
	})
	for _, tt := range []struct {
		name    string
		role    entity.Role
		actor   uuid.UUID
		allowed bool
	}{
		{
			"owner",
			entity.RoleGuest,
			owner,
			true,
		},
		{
			"another guest",
			entity.RoleGuest,
			other,
			false,
		},
		{
			"reception",
			entity.RoleReception,
			uuid.Nil,
			true,
		},
		{
			"operator",
			entity.RoleOperator,
			uuid.Nil,
			true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.Execute(context.Background(), Input{
				Context: bookingdto.Context{
					RequestID: requestID,
					Role:      tt.role,
					ActorID:   tt.actor,
				},
				BookingID: id,
			})
			if (err == nil) != tt.allowed {
				t.Fatalf("error=%v", err)
			}
			if !tt.allowed && !bookingerror.IsCode(err, bookingerror.PermissionDenied) {
				t.Fatal("wrong denial category")
			}
		})
	}
}
