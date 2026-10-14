package available

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gofrs/uuid"

	"booking/internal/domain/vo"
	roomdto "booking/internal/dto/room"
)

type roomsFake struct {
	rooms []roomdto.Room
	err   error
}

func (f roomsFake) List(context.Context, uuid.UUID) ([]roomdto.Room, error) {
	return f.rooms, f.err
}

type storageFake struct {
	free    []int64
	calls   int
	roomIDs []int64
	err     error
}

func (f *storageFake) FreeRooms(_ context.Context, rooms []int64, _ vo.Period) ([]int64, error) {
	f.calls++
	f.roomIDs = rooms
	return f.free, f.err
}

func TestAvailableRooms(t *testing.T) {
	at := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	period, _ := vo.NewPeriod(at, at.Add(12*time.Hour))
	room, short := int64(1), int64(2)
	input := Input{
		HotelID: uuid.Must(uuid.NewV4()),
		Period:  period,
	}
	rooms := []roomdto.Room{
		{
			ID:     room,
			Limits: vo.Limits{Min: 12 * time.Hour},
		},
		{
			ID:     short,
			Limits: vo.Limits{Min: 24 * time.Hour},
		},
	}
	for _, test := range []struct {
		name       string
		occupied   bool
		roomErr    error
		storageErr error
		want       int
		wantCalls  int
	}{
		{
			name:      "free room satisfying duration",
			want:      1,
			wantCalls: 1,
		},
		{
			name:      "occupied room",
			occupied:  true,
			wantCalls: 1,
		},
		{
			name:    "catalog failure",
			roomErr: errors.New("Room unavailable"),
		},
		{
			name:       "database failure",
			storageErr: errors.New("database unavailable"),
			wantCalls:  1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			storage := &storageFake{err: test.storageErr}
			if !test.occupied && test.storageErr == nil {
				storage.free = []int64{room}
			}
			result, err := New(roomsFake{
				rooms: rooms,
				err:   test.roomErr,
			}, storage).Execute(context.Background(), input)
			if (err != nil) != (test.roomErr != nil || test.storageErr != nil) || len(result) != test.want || storage.calls != test.wantCalls {
				t.Fatalf("result=%v err=%v calls=%d", result, err, storage.calls)
			}
			if storage.calls > 0 && (len(storage.roomIDs) != 1 || storage.roomIDs[0] != room) {
				t.Fatal("duration restrictions did not filter catalog candidates")
			}
		})
	}
}
