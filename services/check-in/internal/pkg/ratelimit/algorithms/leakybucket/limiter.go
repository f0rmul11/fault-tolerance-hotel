// Package leakybucket показывает ядро shaper: расчёт равномерного выпуска.
package leakybucket

import "time"

// Limiter хранит виртуальное расписание, а не сами запросы.
// Вызовы последовательные, now не убывает. Для совместного использования
// нужен внешний mutex вокруг Reserve, но не вокруг последующего ожидания.
type Limiter struct {
	interval time.Duration // Расстояние между выпусками, например 100 мс = 10 RPS.
	maxWait  time.Duration // Вместимость очереди, выраженная временем ожидания.
	next     time.Time     // Ближайший ещё не занятый момент выпуска.
}

// New задаёт интервал и число мест ОЖИДАНИЯ. Первый немедленный выпуск
// места не занимает. capacity = 3 и interval = 100 мс дают максимум 300 мс
// ожидания. capacity = 0 отключает очередь. Ошибочные параметры вызывают panic.
func New(interval time.Duration, capacity int) *Limiter {
	if interval <= 0 || capacity < 0 {
		panic("leakybucket: нужны interval > 0 и capacity >= 0")
	}
	if int64(capacity) > int64((1<<63-1)/interval) {
		panic("leakybucket: максимальное ожидание не помещается в time.Duration")
	}

	return &Limiter{
		interval: interval,
		maxWait:  time.Duration(capacity) * interval,
	}
}

// Reserve бронирует выпуск через delay. При ok=false запрос отвергнут.
// Вызывающий код обязан выдержать delay перед обработкой. Здесь нет Sleep,
// исполнения запросов и отмены брони: это только ядро алгоритма расписания.
func (l *Limiter) Reserve(now time.Time) (delay time.Duration, ok bool) {
	release := l.next
	if release.Before(now) {
		// После простоя один запрос выйдет сразу, остальные — с интервалом.
		// Не накапливаем право выпустить пачку запросов одновременно.
		release = now
	}

	delay = release.Sub(now)
	if delay > l.maxWait {
		// Отказ не резервирует слот и не отодвигает следующие запросы.
		return 0, false
	}

	// При четырёх одновременных запросах и interval = 100 мс
	// получатся выпуски сейчас, через 100, 200 и 300 мс.
	l.next = release.Add(l.interval)
	return delay, true
}
