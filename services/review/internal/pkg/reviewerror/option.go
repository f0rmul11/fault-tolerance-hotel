package reviewerror

import "github.com/pkg/errors"

type Option func(*Error)

func WithCode(code int) Option {
	return func(e *Error) {
		e.code = Code(code)
	}
}

func WithMessage(message string) Option {
	return func(e *Error) {
		e.message = message
	}
}

func WithBase(base string) Option {
	return func(e *Error) {
		e.base = errors.New(base)
	}
}

func WithClassification(classification string) Option {
	return func(e *Error) {
		e.classification = classification
	}
}
