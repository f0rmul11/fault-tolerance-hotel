package retry

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// retryableCodeMap отображение строк -> codes.Code
var retryableCodeMap = map[string]codes.Code{
	"DataLoss":          codes.DataLoss,
	"DeadlineExceeded":  codes.DeadlineExceeded,
	"ResourceExhausted": codes.ResourceExhausted,
	"Internal":          codes.Internal,
	"Unavailable":       codes.Unavailable,
}

func isRetryableCode(err error, codes []string, codesSet map[codes.Code]struct{}) bool {
	if err == nil {
		return false
	}

	// Пытаемся получить gRPC-статус
	st, ok := status.FromError(err)
	if !ok {
		// Неизвестная ошибка, не ретраим
		return false
	}

	code := st.Code()

	// Если фильтрация не задана — ничего не ретраим
	if len(codes) == 0 {
		return false
	}

	_, allowed := codesSet[code]
	return allowed
}
