package booking_confirmed

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gofrs/uuid"

	"check-in/internal/application/usecase/checkin/register"
	"check-in/internal/domain/entity"
)

type storageFake struct {
	checkin *entity.CheckIn
}

func (s *storageFake) Create(_ context.Context, c *entity.CheckIn) error {
	s.checkin = c
	return nil
}

func TestConfirmedEventPreservesRoomIdentityAndSnapshot(t *testing.T) {
	bookingID := uuid.Must(uuid.NewV4())
	guestID := uuid.Must(uuid.NewV4())
	for _, tt := range []struct {
		name     string
		roomID   string
		accepted bool
	}{
		{name: "numeric ID", roomID: "11", accepted: true},
		{name: "UUID is not a room ID", roomID: uuid.Must(uuid.NewV4()).String()},
		{name: "zero", roomID: "0"},
		{name: "negative", roomID: "-1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			storage := &storageFake{}
			handler := NewHandler(register.New(storage))
			body, err := json.Marshal(Envelope{
				BookingID: bookingID.String(),
				Payload: BookingConfirmed{
					GuestID:    guestID.String(),
					RoomID:     tt.roomID,
					RoomNumber: "101",
					RoomType:   "Family",
					StartsAt:   "2026-10-14T12:00:00Z",
					EndsAt:     "2026-10-15T12:00:00Z",
				}})
			if err != nil {
				t.Fatal(err)
			}
			err = handler.HandleEvent(context.Background(), body)
			if !tt.accepted {
				if err == nil || storage.checkin != nil {
					t.Fatal("invalid room ID reached storage")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			c := storage.checkin
			if c == nil || c.BookingID() != bookingID || c.GuestID() != guestID || c.ID() == uuid.Nil {
				t.Fatal("lost UUID identity")
			}
			if c.RoomID() != 11 || c.RoomNumber() != "101" || c.RoomType() != "Family" {
				t.Fatal("lost room snapshot")
			}
		})
	}
}
