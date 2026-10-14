package dao

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gofrs/uuid"

	"booking/internal/pkg/xo"
)

func TestRestoreBookingPeriod(t *testing.T) {
	at := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	id := uuid.Must(uuid.NewV4())
	row := xo.Booking{
		ID: id,
		GuestID: uuid.NullUUID{
			UUID:  uuid.Must(uuid.NewV4()),
			Valid: true,
		},
		RoomID:   1,
		StartsAt: at,
		EndsAt:   at.Add(12 * time.Hour),
		RequestID: uuid.NullUUID{
			UUID:  uuid.Must(uuid.NewV4()),
			Valid: true,
		},
		Status: sql.NullString{
			String: "CONFIRMED",
			Valid:  true,
		},
		Version: sql.NullInt64{
			Int64: 4,
			Valid: true,
		},
		CreatedAt: sql.NullTime{
			Time:  at,
			Valid: true,
		},
		UpdatedAt: sql.NullTime{
			Time:  at,
			Valid: true,
		},
	}

	value := Booking{
		Booking: row,
	}
	b, err := value.ConvertTo()
	if err != nil {
		t.Fatal(err)
	}
	if b.ID() != id || b.Version() != 4 || b.OldVersion() != 4 {
		t.Fatal("hydration changed booking identity or version")
	}
	if !b.Period().Start().Equal(at) || !b.Period().End().Equal(at.Add(12*time.Hour)) {
		t.Fatal("hydration changed period boundaries")
	}
	value.EndsAt = value.StartsAt
	if _, err := value.ConvertTo(); err == nil {
		t.Fatal("invalid time interval accepted")
	}
}
