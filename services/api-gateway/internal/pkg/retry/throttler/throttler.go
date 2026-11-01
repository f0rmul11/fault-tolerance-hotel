package throttler

import (
	"log/slog"
	"sync"
)

type Throttler struct {
	max    float64 // максимальное количество токенов
	thresh float64 // пороговое значение, на котором и ниже которого повторы запрещены (иначе будет отброшен retry)
	ratio  float64 // доля, которую мы добавляем при успехе (токен)

	mu     sync.Mutex
	tokens float64 // текущее кол-во токенов
}

// NewThrottler создаем новый throttler
func NewThrottler(max, ratio float64) *Throttler {
	return &Throttler{
		tokens: max,     // изначально полный бак топлива
		max:    max,     // максимальный объем бака
		thresh: max / 2, // загорается предупреждение при исчерпании половины
		ratio:  ratio,
	}
}

// Throttle учитывает один квалифицируемый отказ и проверяет порог retry.
// Вызывается после ошибки, включая последнюю попытку; не при отправке retry.
func (rt *Throttler) Throttle() bool {
	if rt == nil {
		return false
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	// вычитаем токен
	rt.tokens--
	// если ушли в овердрафт - закидываем в 0
	if rt.tokens < 0 {
		rt.tokens = 0
	}

	slog.Debug("throttler: error rpc", "tokens", rt.tokens)
	// если перешли за минимальный порог - retry запрещен, т.к исчерпали бюджет
	return rt.tokens <= rt.thresh
}

// SuccessfulRPC засчитывает успешный rpc запрос
// Нужно для понимания того, жив ли сервис, к которому ходим. Чтобы при исчерпании квоты мы могли ее пополнить
// Ведь ранее мы лишь вычитали из нее токены
func (rt *Throttler) SuccessfulRPC() {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	// добавляем к нашим токенам ratio (восполняем)
	rt.tokens += rt.ratio

	// ограничиваем, чтобы не переполниться хех
	if rt.tokens > rt.max {
		rt.tokens = rt.max
	}

	slog.Debug("throttler: successful rpc", "tokens", rt.tokens)
}
