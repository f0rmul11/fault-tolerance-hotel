package usecase_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gofrs/uuid"

	"booking/internal/application/usecase/booking/activate"
	"booking/internal/application/usecase/booking/cancel"
	"booking/internal/domain/entity"
	"booking/internal/domain/vo"
	checkindto "booking/internal/dto/checkin"
	"booking/internal/events"
	"booking/internal/pkg/event"
)

type bookingStorage struct {
	booking *entity.Booking
	saves   int
}

func (s *bookingStorage) Get(context.Context, uuid.UUID) (*entity.Booking, error) {
	return entity.With(s.booking.ID(),
		entity.WithStatus(s.booking.Status()),
		entity.WithVersion(s.booking.Version()),
		entity.WithPeriod(s.booking.Period()),
		entity.WithUpdatedAt(s.booking.UpdatedAt()),
	), nil
}

func (s *bookingStorage) Save(_ context.Context, bookings ...*entity.Booking) error {
	for _, booking := range bookings {
		s.booking = booking
		s.saves++
	}
	return nil
}

type replyFlusher struct {
	messages event.Events
}

func (f *replyFlusher) Flush(_ context.Context, messages event.Events) error {
	f.messages = append(f.messages, messages...)
	return nil
}

func TestRepeatedCheckInCommands(t *testing.T) {
	at := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	period, err := vo.NewPeriod(at, at.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		action    string
		initial   entity.Status
		want      entity.Status
		wantSaves int
		replyType string
	}{
		{"activate repeatedly", "activate", entity.Confirmed, entity.Active, 1, "booking.activated.v1"},
		{"cancel confirmed repeatedly", "cancel", entity.Confirmed, entity.Canceled, 1, "booking.canceled.v1"},
		{"cancel active repeatedly", "cancel", entity.Active, entity.Canceled, 1, "booking.canceled.v1"},
		{"late activation cannot revive canceled booking", "activate", entity.Canceled, entity.Canceled, 0, "booking.checkin-rejected.v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			storage := &bookingStorage{
				booking: entity.With(uuid.Must(uuid.NewV4()),
					entity.WithStatus(tc.initial),
					entity.WithPeriod(period),
					entity.WithVersion(5),
					entity.WithUpdatedAt(at),
				),
			}
			flusher := &replyFlusher{}
			request := checkindto.Request{
				CorrelationID:  uuid.Must(uuid.NewV4()),
				CheckInID:      uuid.Must(uuid.NewV4()),
				BookingID:      storage.booking.ID(),
				CheckInVersion: 2,
				RequestedAt:    at.Add(time.Hour),
			}
			for attempt := 0; attempt < 3; attempt++ {
				if tc.action == "activate" {
					err = activate.New(storage, flusher).Execute(t.Context(), activate.Input{Request: request})
				} else {
					err = cancel.New(storage, flusher).Execute(t.Context(), cancel.Input{Request: request})
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if storage.saves != tc.wantSaves || storage.booking.Status() != tc.want || storage.booking.Version() != 5+tc.wantSaves {
				t.Fatalf("repeat changed booking: status=%s version=%d saves=%d", storage.booking.Status(), storage.booking.Version(), storage.saves)
			}
			if len(flusher.messages) != 3 {
				t.Fatalf("every attempt must answer Check-in: replies=%d", len(flusher.messages))
			}
			ids := make(map[string]bool)
			for _, message := range flusher.messages {
				var reply events.Base[struct {
					CausationID    string `json:"causation_id"`
					CheckInVersion int    `json:"checkin_version"`
				}]
				if err := json.Unmarshal(message.Body, &reply); err != nil {
					t.Fatal(err)
				}
				if reply.EventType != tc.replyType || reply.AggregateVersion != storage.booking.Version() || reply.Payload.CausationID != request.CorrelationID.String() || reply.Payload.CheckInVersion != request.CheckInVersion {
					t.Fatalf("incorrect reply: %+v", reply)
				}
				if uuid.FromStringOrNil(reply.CorrelationID) == uuid.Nil || ids[reply.CorrelationID] {
					t.Fatal("new reply must have its own correlation ID")
				}
				ids[reply.CorrelationID] = true
			}
		})
	}
}
