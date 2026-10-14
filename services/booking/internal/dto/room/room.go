package room

import (
	"booking/internal/domain/vo"
)

// Room содержит только данные каталога, необходимые сценарию бронирования.
type Room struct {
	ID       int64
	Number   string
	RoomType string
	Limits   vo.Limits
}
