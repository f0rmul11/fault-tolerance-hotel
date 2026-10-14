package message

import (
	"encoding/json"
	"time"
)

// Option ...
type Option func(opt *Message)

// WithID ...
func WithID(id int64) Option {
	return func(message *Message) {
		message.id = id
	}
}

// WithHeaders ...
func WithHeaders(headers json.RawMessage) Option {
	return func(opt *Message) {
		opt.headers = headers
	}
}

// WithMetadata ...
func WithMetadata(meta json.RawMessage) Option {
	return func(message *Message) {
		message.metadata = meta
	}
}

// WithProcessedAt sets processedAt to message
func WithProcessedAt(processedAt time.Time) Option {
	return func(message *Message) {
		message.processedAt = &processedAt
	}
}

// WithErrorsCount sets errorsCount to message
func WithErrorsCount(errorsCount int32) Option {
	return func(message *Message) {
		message.errorsCount = errorsCount
	}
}

// WithErrDesc sets errorDescription to message
func WithErrDesc(errorsDesc string) Option {
	return func(message *Message) {
		message.errDesc = &errorsDesc
	}
}
