package create

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gofrs/uuid"

	"booking/internal/domain/entity"
	"booking/internal/domain/vo"
	bookingdto "booking/internal/dto/booking"
	roomdto "booking/internal/dto/room"
	app "booking/internal/pkg/bookingerror"
)

type storageFake struct {
	row *entity.Booking
	err error
}

func (s *storageFake) FindByKey(context.Context, uuid.UUID, uuid.UUID) (*entity.Booking, error) {
	return s.row, s.err
}

type roomsFake struct {
	calls int
	err   error
}

func (s *roomsFake) Get(_ context.Context, id int64) (roomdto.Room, error) {
	s.calls++
	return roomdto.Room{
		ID:       id,
		Number:   "101",
		RoomType: "Standard",
		Limits:   vo.Limits{Min: 12 * time.Hour},
	}, s.err
}

type confirmerFake struct {
	calls int
	err   error
}

func (s *confirmerFake) Confirm(_ context.Context, booking *entity.Booking, _ uuid.UUID, _ vo.Limits) (*entity.Booking, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return booking, nil
}

func TestCreate(t *testing.T) {
	at := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	period, _ := vo.NewPeriod(at, at.Add(12*time.Hour))
	input := Input{
		Context: bookingdto.Context{
			ActorID:   uuid.Must(uuid.NewV4()),
			RequestID: uuid.Must(uuid.NewV4()),
			Role:      entity.RoleGuest,
		},
		RoomID:         1,
		Period:         period,
		IdempotencyKey: uuid.Must(uuid.NewV4()),
	}
	original, err := entity.NewBooking(
		uuid.Must(uuid.NewV4()),
		input.Context.ActorID,
		input.RoomID,
		uuid.Must(uuid.NewV4()),
		input.Period,
		at,
	)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("connection lost")
	for _, test := range []struct {
		name         string
		replay       bool
		storageErr   error
		roomErr      error
		commitErr    error
		wantErr      bool
		roomCalls    int
		confirmCalls int
	}{
		{
			name:         "new booking",
			roomCalls:    1,
			confirmCalls: 1,
		},
		{
			name:    "replay without room",
			replay:  true,
			roomErr: failure,
		},
		{
			name:       "database read failure",
			storageErr: failure,
			wantErr:    true,
		},
		{
			name:      "room failure before transaction",
			roomErr:   failure,
			wantErr:   true,
			roomCalls: 1,
		},
		{
			name:         "commit failure",
			commitErr:    failure,
			wantErr:      true,
			roomCalls:    1,
			confirmCalls: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			storage := &storageFake{err: test.storageErr}
			if test.replay {
				storage.row = original
			}
			rooms := &roomsFake{err: test.roomErr}
			confirmer := &confirmerFake{err: test.commitErr}
			result, err := New(storage, rooms, confirmer).Execute(context.Background(), input)
			if (err != nil) != test.wantErr || rooms.calls != test.roomCalls || confirmer.calls != test.confirmCalls {
				t.Fatalf("err=%v room=%d confirm=%d", err, rooms.calls, confirmer.calls)
			}
			if test.wantErr && result != nil {
				t.Fatal("failed operation returned an unsaved booking")
			}
			if test.replay && result != original {
				t.Fatal("replay did not return the original response")
			}
			if !test.wantErr && (result.ID() == uuid.Nil || result.Status() != entity.Confirmed) {
				t.Fatal("invalid successful booking")
			}
		})
	}

	input.IdempotencyKey = uuid.Nil
	_, err = New(&storageFake{}, &roomsFake{}, &confirmerFake{}).Execute(
		context.Background(),
		input,
	)
	if !app.IsCode(err, app.InvalidArgument) {
		t.Fatal("missing idempotency key accepted")
	}
}
