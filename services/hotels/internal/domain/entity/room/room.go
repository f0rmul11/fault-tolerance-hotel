package room

import (
	"slices"
	"time"

	"github.com/gofrs/uuid"
)

type Room struct {
	id               int64
	hotelID          uuid.UUID
	number           string
	name             string
	roomType         string
	description      string
	capacity         int
	roomCount        int
	bedCount         int
	areaSquareMeters int
	amenities        []string
	minDuration      time.Duration
	maxDuration      time.Duration
}

func (v *Room) ID() int64 {
	return v.id
}

func (v *Room) HotelID() uuid.UUID {
	return v.hotelID
}

func (v *Room) Number() string {
	return v.number
}

func (v *Room) Name() string {
	return v.name
}

func (v *Room) RoomType() string {
	return v.roomType
}

func (v *Room) Description() string {
	return v.description
}

func (v *Room) Capacity() int {
	return v.capacity
}

func (v *Room) RoomCount() int {
	return v.roomCount
}

func (v *Room) BedCount() int {
	return v.bedCount
}

func (v *Room) AreaSquareMeters() int {
	return v.areaSquareMeters
}

func (v *Room) Amenities() []string {
	return slices.Clone(v.amenities)
}

func (v *Room) MinDuration() time.Duration {
	return v.minDuration
}

func (v *Room) MaxDuration() time.Duration {
	return v.maxDuration
}
