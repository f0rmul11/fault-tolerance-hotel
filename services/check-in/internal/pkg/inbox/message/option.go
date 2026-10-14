package message

import (
	"time"
)

type Option func(opt *Message)

// WithID устанавливает id
func WithID(id int64) Option {
	return func(message *Message) {
		message.id = id
	}
}

// WithHeaders устанавливает заголовки
func WithHeaders(headers []byte) Option {
	return func(opt *Message) {
		opt.headers = headers
	}
}

// WithMetadata устанавливает метаинформацию
func WithMetadata(meta []byte) Option {
	return func(message *Message) {
		message.metadata = meta
	}
}

// WithProcessedAt устанавливает processedAt
func WithProcessedAt(processedAt time.Time) Option {
	return func(message *Message) {
		message.processedAt = &processedAt
	}
}

// WithErrorsCount устанавливает errorsCount
func WithErrorsCount(errorsCount int32) Option {
	return func(message *Message) {
		message.errorsCount = errorsCount
	}
}

// WithErrDesc устанавливает errorDescription
func WithErrDesc(errorsDesc string) Option {
	return func(message *Message) {
		message.errDesc = &errorsDesc
	}
}
