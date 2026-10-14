package dao

import (
	"check-in/internal/domain/entity"
	"check-in/internal/domain/vo"
	"check-in/internal/pkg/xo"
)

type CheckIn struct{ xo.Checkin }

func (r CheckIn) ConvertTo() (*entity.CheckIn, error) {
	period, err := vo.NewPeriod(r.StartsAt.Time, r.EndsAt.Time)
	if err != nil {
		return nil, err
	}
	return entity.With(r.ID,
		entity.WithBookingID(r.BookingID.UUID),
		entity.WithRoomID(r.RoomID.Int64),
		entity.WithRoomNumber(r.RoomNumber.String),
		entity.WithRoomType(r.RoomType.String),
		entity.WithGuestID(r.GuestID.UUID),
		entity.WithPeriod(period),
		entity.WithStatus(entity.Status(r.Status.String)),
		entity.WithPreviousStatus(entity.Status(r.PreviousStatus.String)),
		entity.WithVersion(int(r.Version.Int64)),
		entity.WithActorID(r.ActorID.UUID),
		entity.WithRequestID(r.RequestID.UUID),
		entity.WithRequestedAt(r.RequestedAt.Time),
		entity.WithFailure(r.Failure.String),
	), nil
}
