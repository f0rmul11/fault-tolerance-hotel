// Package slidingwindowcounter показывает приближённое скользящее окно.
package slidingwindowcounter

import "time"

// Limiter хранит два счётчика, а не журнал запросов и не массив сегментов.
// Вызовы последовательные, now не убывает. Для совместного использования
// нужен внешний mutex вокруг всего Allow.
type Limiter struct {
	limit    int
	window   time.Duration
	start    time.Time
	previous int
	current  int
}

// New задаёт число разрешённых запросов за окно; параметры должны быть > 0.
func New(limit int, window time.Duration) *Limiter {
	if limit <= 0 || window <= 0 {
		panic("slidingwindowcounter: нужны limit > 0 и window > 0")
	}

	return &Limiter{
		limit:  limit,
		window: window,
	}
}

// Allow проверяет взвешенную оценку с учётом нового запроса.
func (l *Limiter) Allow(now time.Time) bool {
	// Окна привязаны к общей сетке времени, а не к первому запросу.
	start := now.Truncate(l.window)
	if !start.Equal(l.start) {
		// Старое previous уже не входит в новое скользящее окно.
		l.previous = 0
		if start.Equal(l.start.Add(l.window)) {
			// Соседнее окно: завершившийся current становится previous.
			l.previous = l.current
		}
		// Если пропущено два окна или больше, current тоже устарел.
		l.start = start
		l.current = 0
	}

	// Через 15 с минутного окна вес предыдущего равен 1 - 15/60 = 0.75.
	// Оба Duration выражены в наносекундах: единицы должны совпадать.
	weight := 1 - float64(now.Sub(l.start))/float64(l.window)
	estimate := float64(l.previous)*weight + float64(l.current)
	// При estimate = 4.5 и limit = 5 следующий запрос уже не помещается.
	// Приведение estimate к int потеряло бы дробную часть и пропустило его.
	if estimate+1 > float64(l.limit) {
		return false
	}

	l.current++
	return true
}
