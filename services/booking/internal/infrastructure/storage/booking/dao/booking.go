package dao

import (
	"booking/internal/domain/entity"
	"booking/internal/domain/vo"
	app "booking/internal/pkg/bookingerror"
	"booking/internal/pkg/xo"
)

type Booking struct {
	xo.Booking
}

func (row Booking) ConvertTo() (*entity.Booking, error) {
	period, err := vo.NewPeriod(row.StartsAt, row.EndsAt)
	if err != nil {
		return nil, app.NewInternalError("Некорректный период брони в хранилище", err)
	}
	return entity.With(row.ID,
		entity.WithRoomID(row.RoomID),
		entity.WithRoomNumber(row.RoomNumber.String),
		entity.WithRoomType(row.RoomType.String),
		entity.WithPeriod(period),
		entity.WithCreatedAt(row.CreatedAt.Time),
		entity.WithGuestID(row.GuestID.UUID),
		entity.WithRequestID(row.RequestID.UUID),
		entity.WithStatus(entity.Status(row.Status.String)),
		entity.WithVersion(int(row.Version.Int64)),
		entity.WithUpdatedAt(row.UpdatedAt.Time),
	), nil
}
