package retry

type routingError struct {
	cause     error
	code      string
	permanent bool
}

func (e *routingError) Error() string {
	return e.cause.Error()
}

func (e *routingError) Unwrap() error {
	return e.cause
}

// DLQErr marks a permanent failure for direct delivery to the dead-letter topic.
func DLQErr(code string, err error) error {
	return &routingError{
		cause:     err,
		code:      code,
		permanent: true,
	}
}

// RetryErr carries a diagnostic code and a message safe to show in Kafka headers.
func RetryErr(code string, err error) error {
	return &routingError{
		cause: err,
		code:  code,
	}
}
