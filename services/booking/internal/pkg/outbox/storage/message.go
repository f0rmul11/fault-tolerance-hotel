package storage

import "booking/internal/pkg/outbox/message"

// Offset задаёт позицию сообщения в одной партиции топика.
type Offset struct {
	TransactionID uint64
	ID            int64
}

type PendingMessage struct {
	message.Message
	Offset
}

type Page struct {
	Start    Offset // Подтверждённая позиция на момент блокировки.
	Messages []PendingMessage
}
