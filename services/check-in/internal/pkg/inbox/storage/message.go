package storage

import (
	"database/sql"
	"time"

	"check-in/internal/pkg/inbox/message"
)

type Message struct {
	ID            int64          `db:"id" json:"id"`                         // id
	EntityID      string         `db:"entity_id" json:"entity_id"`           // entity_id
	CorrelationID string         `db:"correlation_id" json:"correlation_id"` // correlation_id
	Topic         string         `db:"topic" json:"topic"`                   // topic
	Key           []byte         `db:"key" json:"key"`                       // key
	Body          []byte         `db:"body" json:"body"`                     // body
	Headers       []byte         `db:"header" json:"header"`                 // header
	Metadata      []byte         `db:"metadata" json:"metadata"`             // metadata
	CreatedAt     time.Time      `db:"created_at" json:"created_at"`         // created_at
	ProcessedAt   sql.NullTime   `db:"processed_at" json:"processed_at"`     // processed_at
	ErrorsCount   int32          `db:"errors_count" json:"errors_count"`     // errors_count
	ErrorDesc     sql.NullString `db:"error_desc" json:"error_desc"`         // error_desc
}

func (m Message) Convert() *message.Message {
	opts := []message.Option{
		message.WithID(m.ID),
		message.WithHeaders(m.Headers),
		message.WithMetadata(m.Metadata),
		message.WithErrorsCount(m.ErrorsCount),
	}

	if m.ProcessedAt.Valid {
		opts = append(opts, message.WithProcessedAt(m.ProcessedAt.Time))
	}
	if m.ErrorDesc.Valid {
		opts = append(opts, message.WithErrDesc(m.ErrorDesc.String))
	}

	return message.NewMessage(m.EntityID, m.CorrelationID, m.Topic, m.Key, m.Body, m.CreatedAt, opts...)
}
