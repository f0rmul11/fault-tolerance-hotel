// Package tokenbucket показывает ядро token bucket без фонового пополнения.
package tokenbucket

import (
	"math"
	"time"
)

// Limiter хранит один bucket. Вызовы последовательные, now не убывает.
// Для совместного использования нужен внешний mutex вокруг всего Allow.
type Limiter struct {
	rate     float64 // Токенов в секунду.
	capacity float64 // Максимальный запас, он же мгновенный burst.
	tokens   float64 // Дробную часть сохраняем между запросами.
	last     time.Time
}

// New создаёт полный bucket. Некорректная конфигурация — ошибка программиста.
func New(rate float64, capacity int) *Limiter {
	if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) || capacity <= 0 {
		panic("tokenbucket: нужны конечная положительная скорость и capacity > 0")
	}

	return &Limiter{
		rate:     rate,
		capacity: float64(capacity),
		tokens:   float64(capacity),
	}
}

// Allow расходует один токен или сразу отказывает; ожидания здесь нет.
func (l *Limiter) Allow(now time.Time) bool {
	// Считаем пополнение только при обращении. При 2 токенах/с за 250 мс
	// накопится 0.5 токена. Ticker и goroutine для этого не нужны.
	elapsed := now.Sub(l.last).Seconds()
	l.tokens = math.Min(l.capacity, l.tokens+elapsed*l.rate)
	l.last = now

	// При первом обращении bucket уже полон; долгий простой тоже не даёт
	// накопить больше capacity. Отказ сохраняет дробный остаток.
	if l.tokens < 1 {
		return false
	}

	l.tokens--
	return true
}
