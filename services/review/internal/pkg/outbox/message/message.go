package message

import (
	"time"

	"github.com/samber/lo"
)

type Message struct {
	id       int64
	entityID string

	topic string

	key      []byte
	body     []byte
	headers  []byte
	metadata []byte

	createdAt   time.Time
	processedAt *time.Time

	errorsCount int32
	errDesc     *string
}

func NewMessage(
	entityID string,
	topic string,
	key, body []byte,
	options ...Option,
) *Message {
	message := &Message{
		entityID:  entityID,
		topic:     topic,
		key:       key,
		body:      body,
		createdAt: time.Now(),
	}

	for _, opt := range options {
		opt(message)
	}

	return message
}

// GetID возвращает id
func (m *Message) GetID() int64 {
	return m.id
}

// GetEntityID возвращает entityID
func (m *Message) GetEntityID() string {
	return m.entityID
}

// GetTopic возвращает topic
func (m *Message) GetTopic() string {
	return m.topic
}

// GetKey возвращает key
func (m *Message) GetKey() []byte {
	return m.key
}

// GetBody возвращает body
func (m *Message) GetBody() []byte {
	return m.body
}

// GetHeaders возвращает headers
func (m *Message) GetHeaders() []byte {
	return m.headers
}

// GetMetadata возвращает metadata
func (m *Message) GetMetadata() []byte {
	return lo.Ternary(m.metadata == nil, []byte("{}"), m.metadata)
}

// GetCreatedAt возвращает createdAt
func (m *Message) GetCreatedAt() time.Time {
	return m.createdAt
}

// GetProcessedAt возвращает processedAt
func (m *Message) GetProcessedAt() *time.Time {
	return m.processedAt
}

// GetErrorsCount возвращает errorsCount
func (m *Message) GetErrorsCount() int32 {
	return m.errorsCount
}

// GetErrorDesc возвращает описание последней ошибки, если такова была или nil, если не было
func (m *Message) GetErrorDesc() *string {
	return m.errDesc
}

// MarkAsProcessed помечает сообщение как обработанное
func (m *Message) MarkAsProcessed() {
	m.processedAt = lo.ToPtr(time.Now().UTC())
	m.errorsCount = 0
	m.errDesc = nil
}

// SetError устанавливает произошедшую ошибку
func (m *Message) SetError(err error) {
	m.errorsCount++
	m.errDesc = lo.ToPtr(err.Error())
}
