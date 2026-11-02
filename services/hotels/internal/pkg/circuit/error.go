package circuit

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	ErrCircuitIsOpen = errors.New("circuit is open")

	ErrTooManyRequests = errors.New("too many requests through circuit")
)

// errorCodeMap отображение строк -> codes.Code
var errorCodeMap = map[string]codes.Code{
	"Unknown":           codes.Unknown,
	"DeadlineExceeded":  codes.DeadlineExceeded,
	"ResourceExhausted": codes.ResourceExhausted,
	"Internal":          codes.Internal,
	"Unavailable":       codes.Unavailable,
}

// excludedError preserves the caller's result without counting cancellation
// as a successful half-open probe.
type excludedError struct {
	error
}

func triggerOnError(err error, codesSet map[codes.Code]struct{}) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		err = status.FromContextError(err).Err()
	}

	// Пытаемся получить gRPC-статус
	st, ok := status.FromError(err)
	if !ok {
		// Не gRPC-ошибка — считаем за сбой, если список кодов не задан
		return len(codesSet) == 0
	}

	code := st.Code()

	// Если фильтрация не задана — считаем всё ошибками
	if len(codesSet) == 0 {
		return true
	}

	// Проверяем вхождение в список "плохих" кодов
	_, allowed := codesSet[code]
	return allowed
}

func failureCode(name string) (codes.Code, bool) {
	for spelling, code := range errorCodeMap {
		if strings.EqualFold(strings.ReplaceAll(name, "_", ""), spelling) {
			return code, true
		}
	}
	return codes.Unknown, false
}
