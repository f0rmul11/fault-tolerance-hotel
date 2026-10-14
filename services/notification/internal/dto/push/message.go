package push

import "github.com/gofrs/uuid"

// Message — текст уведомления и идентификаторы для трассировки.
type Message struct {
	CorrelationID uuid.UUID
	BookingID     uuid.UUID
	Title         string
	Text          string
}
