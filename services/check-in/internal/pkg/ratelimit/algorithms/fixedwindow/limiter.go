// Package fixedwindow показывает счётчик в фиксированных окнах [start, end).
package fixedwindow

import "time"

// Limiter хранит одно окно. Вызовы последовательные, now не убывает.
// Для совместного использования нужен внешний mutex вокруг всего Allow.
type Limiter struct {
	limit  int
	window time.Duration
	start  time.Time
	count  int
}

// New задаёт число разрешённых запросов за окно; параметры должны быть > 0.
func New(limit int, window time.Duration) *Limiter {
	if limit <= 0 || window <= 0 {
		panic("fixedwindow: нужны limit > 0 и window > 0")
	}

	return &Limiter{
		limit:  limit,
		window: window,
	}
}

// Allow считает только разрешённые запросы.
func (l *Limiter) Allow(now time.Time) bool {
	// Окно привязано к временной сетке, а не к первому запросу:
	// для минуты 12:34:59 принадлежит [12:34:00, 12:35:00).
	start := now.Truncate(l.window)
	if !start.Equal(l.start) {
		l.start = start
		l.count = 0
	}

	if l.count >= l.limit {
		return false
	}

	l.count++
	return true
}
