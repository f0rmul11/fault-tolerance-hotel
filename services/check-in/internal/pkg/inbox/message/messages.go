package message

import (
	"time"

	"github.com/samber/lo"
)

type Messages []*Message

// IsEmpty признак пустоты батча
func (b Messages) IsEmpty() bool {
	return len(b) == 0
}

// Succeed возвращает успешно обработанные сообщения без ошибок
func (b Messages) Succeed() Messages {
	return lo.Filter(b, func(message *Message, i int) bool {
		return message.errorsCount == 0
	})
}

// MarkAsProcessed помечает батч как обработанный
func (b Messages) MarkAsProcessed() {
	for _, msg := range b {
		msg.MarkAsProcessed()
	}
}

// AnErrorOccurred помечает батч как необработанный
func (b Messages) AnErrorOccurred(err error) {
	for _, m := range b {
		m.SetError(err)
	}
}

// Numbers отдает слайс номеров сообщений батча
func (b Messages) Numbers() []int64 {
	return lo.Map(b, func(_ *Message, i int) int64 {
		return int64(i)
	})
}

// IDs отдает слайс id сообщений батча
func (b Messages) IDs() []int64 {
	return lo.Map(b, func(msg *Message, _ int) int64 {
		return msg.GetID()
	})
}

// EntitiesIDs отдает слайс entity_id сообщений батча
func (b Messages) EntitiesIDs() []string {
	return lo.Map(b, func(msg *Message, _ int) string {
		return msg.GetEntityID()
	})
}

// CorrelationIDs отдает слайс correlation_id сообщений батча
func (b Messages) CorrelationIDs() []string {
	return lo.Map(b, func(msg *Message, _ int) string {
		return msg.GetCorrelationID()
	})
}

// Topics отдает слайс topic сообщений батча
func (b Messages) Topics() []string {
	return lo.Map(b, func(msg *Message, _ int) string {
		return msg.GetTopic()
	})
}

// Keys отдает слайс key сообщений батча
func (b Messages) Keys() [][]byte {
	return lo.Map(b, func(msg *Message, _ int) []byte {
		return msg.GetKey()
	})
}

// Bodies отдает слайс body сообщений батча
func (b Messages) Bodies() [][]byte {
	return lo.Map(b, func(msg *Message, _ int) []byte {
		return msg.GetBody()
	})
}

// Headers отдает слайс header сообщений батча
func (b Messages) Headers() []string {
	return lo.Map(b, func(msg *Message, _ int) string {
		return string(msg.GetHeaders())
	})
}

// Metadata отдает слайс metadata сообщений батча
func (b Messages) Metadata() []string {
	return lo.Map(b, func(msg *Message, _ int) string {
		return string(msg.GetMetadata())
	})
}

// CreatedAt отдает слайс created_at сообщений батча
func (b Messages) CreatedAt() []time.Time {
	return lo.Map(b, func(msg *Message, _ int) time.Time {
		return msg.GetCreatedAt()
	})
}

// ProcessedAt отдает слайс processed_at сообщений батча
func (b Messages) ProcessedAt() []*time.Time {
	return lo.Map(b, func(msg *Message, _ int) *time.Time {
		return msg.GetProcessedAt()
	})
}

// ErrorsCounts отдает слайс error_count сообщений батча
func (b Messages) ErrorsCounts() []int32 {
	return lo.Map(b, func(msg *Message, _ int) int32 {
		return msg.GetErrorsCount()
	})
}

// ErrorsDesc отдает слайс error_count сообщений батча
func (b Messages) ErrorsDesc() []*string {
	return lo.Map(b, func(msg *Message, _ int) *string {
		return msg.GetErrorDesc()
	})
}
