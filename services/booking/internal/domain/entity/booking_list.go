package entity

import "booking/internal/domain/vo"

// BookingList — согласованный снимок броней гостя и версии списка.
type BookingList struct {
	Bookings []*Booking
	Version  vo.Version
}
