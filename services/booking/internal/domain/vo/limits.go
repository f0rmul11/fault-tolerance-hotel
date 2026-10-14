package vo

import "time"

// Limits — условия длительности бронирования из каталога Room.
// Нулевой Max означает отсутствие верхней границы.
type Limits struct {
	Min time.Duration
	Max time.Duration
}

func (l Limits) Allows(period Period) bool {
	if l.Min <= 0 || (l.Max != 0 && l.Max < l.Min) {
		return false
	}
	duration := period.Duration()
	return duration >= l.Min && (l.Max == 0 || duration <= l.Max)
}
