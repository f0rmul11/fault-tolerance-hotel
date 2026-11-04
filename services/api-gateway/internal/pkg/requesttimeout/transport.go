package requesttimeout

import (
	"context"
	"net/http"
	"time"
)

// Transport передаёт оставшийся бюджет HTTP-зависимости с Middleware.
// Контекст запроса также ограничивает ожидание на вызывающей стороне.
// Использовать явно для внутренних вызовов; сам по себе заголовок не отменяет работу.
type Transport struct {
	Base http.RoundTripper
}

func (t Transport) RoundTrip(request *http.Request) (*http.Response, error) {
	ctx := request.Context()
	err := ctx.Err()
	remaining := time.Duration(0)
	deadline, hasDeadline := ctx.Deadline()
	if err == nil && hasDeadline {
		remaining = time.Until(deadline)
		if remaining <= 0 {
			err = context.DeadlineExceeded
		}
	}
	if err != nil {
		// RoundTripper обязан закрыть тело даже при отказе до отправки.
		if request.Body != nil {
			_ = request.Body.Close()
		}
		return nil, err
	}

	// Не изменяем заголовки исходного запроса: его может повторно использовать caller.
	outgoing := request.Clone(ctx)
	outgoing.Header.Del(Header)
	if hasDeadline {
		if outgoing.Header == nil {
			outgoing.Header = make(http.Header)
		}
		outgoing.Header.Set(Header, remaining.String())
	}
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(outgoing)
}
