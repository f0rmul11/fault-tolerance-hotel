package dao

import (
	"database/sql"
	"time"

	"github.com/samber/lo"

	entity "hotels/internal/domain/entity/room"
	"hotels/internal/pkg/xo"
)

type Room struct {
	xo.Room
}

func (row Room) ConvertTo() *entity.Room {
	return entity.With(row.ID,
		entity.WithHotelID(row.HotelID),
		entity.WithNumber(row.Number.String),
		entity.WithName(row.Name.String),
		entity.WithRoomType(row.RoomType.String),
		entity.WithDescription(row.Description.String),
		entity.WithCapacity(int(row.Capacity.Int64)),
		entity.WithRoomCount(int(row.RoomCount.Int64)),
		entity.WithBedCount(int(row.BedCount.Int64)),
		entity.WithAreaSquareMeters(int(row.AreaSquareMeters.Int64)),
		entity.WithAmenities(lo.Map(row.Amenities, func(value sql.NullString, _ int) string {
			return value.String
		})),
		entity.WithMinDuration(time.Duration(row.MinDurationSeconds.Int64)*time.Second),
		entity.WithMaxDuration(time.Duration(row.MaxDurationSeconds.Int64)*time.Second),
	)
}
