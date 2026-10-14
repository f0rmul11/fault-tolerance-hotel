package reviewerror

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/samber/lo"
)

const (
	grpc     string = "gRPC"
	internal string = "internal"
)

type Code int

const (
	External           Code = 0
	NotFound           Code = 1
	Conflict           Code = 2
	FailedPrecondition Code = 3
	InvalidArgument    Code = 4
	Internal           Code = 5
)

type Error struct {
	code           Code
	classification string
	message        string
	base           error
}

func (err *Error) Code() Code {
	return err.code
}

func (err *Error) Classification() string {
	return err.classification
}

func (err *Error) Message() string {
	return err.message
}

func (err *Error) BaseMessage() string {
	return err.base.Error()
}

func (err *Error) Error() string {
	return err.message
}

func (err *Error) Unwrap() error {
	if err.base == nil {
		return errors.New(err.Error())
	}

	return fmt.Errorf("%s : %s", err.message, err.base.Error())
}

func New(opts ...Option) *Error {
	err := new(Error)

	for _, opt := range opts {
		opt(err)
	}
	return err
}

func NewInternalError(message string, base error) error {
	return &Error{
		code:           Internal,
		classification: internal,
		message:        message,
		base:           base,
	}
}

func NewExternalErr(message string, base error) error {
	return &Error{
		code:           External,
		classification: grpc,
		message:        message,
		base:           base,
	}
}

func NewNotFoundErr(message string, base error) error {
	return &Error{
		code:           NotFound,
		classification: internal,
		message:        message,
		base:           base,
	}
}

func NewConflictErr(message string) error {
	return &Error{
		code:           Conflict,
		classification: internal,
		message:        message,
	}
}

func NewBusinessErr(message string) error {
	return &Error{
		code:           FailedPrecondition,
		classification: internal,
		message:        message,
	}
}

func NewInvalidArgumentErr(message string) error {
	return &Error{
		code:           InvalidArgument,
		classification: internal,
		message:        message,
	}
}

func As(err error) (*Error, bool) {
	return lo.ErrorsAs[*Error](err)
}

func IsCode(err error, code Code) bool {
	casted, ok := As(err)
	if !ok {
		return false
	}

	return casted.code == code
}

func LogWrap(err error) {
	if err != nil {
		slog.Error(err.Error())
	}
}
