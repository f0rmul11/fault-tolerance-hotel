package leakybucket

import (
	"testing"
	"time"
)

func TestReserve(t *testing.T) {
	type step struct {
		at    time.Duration
		delay time.Duration
		ok    bool
	}

	tests := []struct {
		name     string
		capacity int
		steps    []step
	}{
		{
			name:     "burst_превращается_в_равномерный_выпуск",
			capacity: 3,
			steps: []step{
				{at: 0, delay: 0, ok: true},
				{at: 0, delay: 100 * time.Millisecond, ok: true},
				{at: 0, delay: 200 * time.Millisecond, ok: true},
				{at: 0, delay: 300 * time.Millisecond, ok: true},
				{at: 0, delay: 0, ok: false}, // Мест ожидания больше нет.
				{at: 0, delay: 0, ok: false},
				{at: 100 * time.Millisecond, delay: 300 * time.Millisecond, ok: true},
				// Отказы не сдвинули расписание: выпуск по-прежнему в 400 мс.
			},
		},
		{
			name:     "часть_интервала_не_освобождает_целое_место",
			capacity: 1,
			steps: []step{
				{at: 0, delay: 0, ok: true},
				{at: 0, delay: 100 * time.Millisecond, ok: true},
				{at: 100*time.Millisecond - time.Nanosecond, delay: 0, ok: false},
				{at: 100 * time.Millisecond, delay: 100 * time.Millisecond, ok: true},
			},
		},
		{
			name:     "простой_не_создаёт_выходной_burst",
			capacity: 2,
			steps: []step{
				{at: 0, delay: 0, ok: true},
				{at: time.Hour, delay: 0, ok: true},
				{at: time.Hour, delay: 100 * time.Millisecond, ok: true},
				{at: time.Hour, delay: 200 * time.Millisecond, ok: true},
				{at: time.Hour, delay: 0, ok: false},
			},
		},
		{
			name:     "очередь_отключена",
			capacity: 0,
			steps: []step{
				{at: 0, delay: 0, ok: true},
				{at: 0, delay: 0, ok: false},
				{at: 99 * time.Millisecond, delay: 0, ok: false},
				{at: 100 * time.Millisecond, delay: 0, ok: true},
			},
		},
		{
			name:     "задержка_считается_от_момента_прихода",
			capacity: 1,
			steps: []step{
				{at: 0, delay: 0, ok: true},
				{at: 40 * time.Millisecond, delay: 60 * time.Millisecond, ok: true},
				{at: 150 * time.Millisecond, delay: 50 * time.Millisecond, ok: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := New(100*time.Millisecond, tt.capacity)
			start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

			for i, s := range tt.steps {
				delay, ok := limiter.Reserve(start.Add(s.at))
				if ok {
					t.Logf("приход=%-8v ожидание=%-8v выпуск=%v", s.at, delay, s.at+delay)
				} else {
					t.Logf("приход=%-8v отказ: очередь заполнена", s.at)
				}

				if ok != s.ok || delay != s.delay {
					t.Fatalf("шаг %d: (delay=%v, ok=%v), ожидается (%v, %v)", i, delay, ok, s.delay, s.ok)
				}
			}
		})
	}
}

func TestNewInvalidConfig(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		capacity int
	}{
		{name: "нулевой_интервал", interval: 0, capacity: 1},
		{name: "отрицательный_интервал", interval: -time.Second, capacity: 1},
		{name: "отрицательная_очередь", interval: time.Second, capacity: -1},
		{name: "переполнение_максимального_ожидания", interval: time.Duration(1<<63 - 1), capacity: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("ожидается panic для ошибочной конфигурации")
				}
			}()

			New(tt.interval, tt.capacity)
		})
	}
}
