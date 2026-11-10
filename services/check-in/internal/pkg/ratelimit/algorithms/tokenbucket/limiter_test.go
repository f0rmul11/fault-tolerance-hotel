package tokenbucket

import (
	"math"
	"testing"
	"time"
)

func TestAllow(t *testing.T) {
	type step struct {
		at       time.Duration
		requests int
		allowed  int
	}

	tests := []struct {
		name     string
		rate     float64
		capacity int
		steps    []step
	}{
		{
			name:     "burst_и_дробное_пополнение",
			rate:     2,
			capacity: 3,
			steps: []step{
				{at: 0, requests: 4, allowed: 3},
				{at: 250 * time.Millisecond, requests: 2, allowed: 0}, // 0.5 токена.
				{at: 500 * time.Millisecond, requests: 2, allowed: 1}, // Доли не потерялись.
				{at: 750 * time.Millisecond, requests: 1, allowed: 0},
				{at: time.Second, requests: 2, allowed: 1},
			},
		},
		{
			name:     "простой_не_увеличивает_capacity",
			rate:     2,
			capacity: 3,
			steps: []step{
				{at: 0, requests: 3, allowed: 3},
				{at: time.Hour, requests: 10, allowed: 3},
				{at: time.Hour, requests: 1, allowed: 0},
			},
		},
		{
			name:     "скорость_меньше_одного_токена_в_секунду",
			rate:     0.5,
			capacity: 1,
			steps: []step{
				{at: 0, requests: 2, allowed: 1},
				{at: time.Second, requests: 1, allowed: 0},
				{at: 2 * time.Second, requests: 1, allowed: 1},
			},
		},
		{
			name:     "ровно_на_границе_нового_токена",
			rate:     1,
			capacity: 1,
			steps: []step{
				{at: 0, requests: 1, allowed: 1},
				{at: time.Second - time.Nanosecond, requests: 1, allowed: 0},
				{at: time.Second, requests: 2, allowed: 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := New(tt.rate, tt.capacity)
			start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

			// Строки — последовательность событий одного bucket, не независимые кейсы.
			for _, s := range tt.steps {
				allowed := 0
				for i := 0; i < s.requests; i++ {
					if limiter.Allow(start.Add(s.at)) {
						allowed++
					}
				}

				t.Logf("t=%-8v запросов=%d пропущено=%d отказов=%d", s.at, s.requests, allowed, s.requests-allowed)
				if allowed != s.allowed {
					t.Fatalf("t=%v: пропущено %d, ожидается %d", s.at, allowed, s.allowed)
				}
			}
		})
	}
}

func TestNewInvalidConfig(t *testing.T) {
	tests := []struct {
		name     string
		rate     float64
		capacity int
	}{
		{name: "нулевая_скорость", rate: 0, capacity: 1},
		{name: "отрицательная_скорость", rate: -1, capacity: 1},
		{name: "NaN", rate: math.NaN(), capacity: 1},
		{name: "бесконечность", rate: math.Inf(1), capacity: 1},
		{name: "нулевая_ёмкость", rate: 1, capacity: 0},
		{name: "отрицательная_ёмкость", rate: 1, capacity: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("ожидается panic для ошибочной конфигурации")
				}
			}()

			New(tt.rate, tt.capacity)
		})
	}
}
