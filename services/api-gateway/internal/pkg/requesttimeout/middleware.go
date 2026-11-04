package requesttimeout

import (
	"context"
	"net/http"
	"time"
)

// MaxDuration — политика входа Gateway, а не правило HTTP propagation.
const MaxDuration = 5 * time.Second

// Header передаёт оставшуюся длительность (например "3s"), не абсолютную дату.
// Это соглашение HTTP-сервисов; формат grpc-timeout намеренно не используется.
const Header = "X-Request-Timeout"

// Middleware восстанавливает deadline из бюджета вызывающей стороны.
// Без заголовка контекст не меняется. Нулевой бюджет сразу истекает.
// Как и в gRPC, обработчик обязан учитывать отмену своего контекста.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values(Header)
		if len(values) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		if len(values) != 1 {
			http.Error(w, "Заголовок X-Request-Timeout должен содержать одну длительность", http.StatusBadRequest)
			return
		}
		duration, err := time.ParseDuration(values[0])
		if err != nil || duration < 0 {
			http.Error(w, "Некорректный X-Request-Timeout: ожидается неотрицательная длительность", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), duration)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Limit задаёт локальный бюджет на входе приложения, не продлевая более ранний deadline.
// Внутренним сервисам для propagation достаточно одного Middleware.
func Limit(duration time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), duration)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
