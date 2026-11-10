package slidingwindowcounter

import (
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
		name  string
		steps []step
	}{
		{
			name: "предыдущее_окно_плавно_теряет_вес",
			steps: []step{
				{at: 0, requests: 5, allowed: 4},
				{at: time.Second, requests: 1, allowed: 0},             // Вес 1: оценка 4.
				{at: 1250 * time.Millisecond, requests: 2, allowed: 1}, // 4*0.75 + 0 + 1 = 4.
				{at: 1500 * time.Millisecond, requests: 2, allowed: 1}, // 4*0.5 + 1 + 1 = 4.
				{at: 2 * time.Second, requests: 3, allowed: 2},         // Предыдущее теперь 2.
			},
		},
		{
			name: "дробную_оценку_нельзя_округлять_вниз",
			steps: []step{
				{at: 0, requests: 4, allowed: 4},
				{at: 1125 * time.Millisecond, requests: 1, allowed: 0}, // 3.5 + 1 > 4.
				{at: 1250*time.Millisecond - time.Nanosecond, requests: 1, allowed: 0},
				{at: 1250 * time.Millisecond, requests: 1, allowed: 1},
			},
		},
		{
			name: "после_двух_окон_старый_счётчик_не_переносится",
			steps: []step{
				{at: 0, requests: 4, allowed: 4},
				{at: 2 * time.Second, requests: 5, allowed: 4},
				{at: time.Hour, requests: 5, allowed: 4},
			},
		},
		{
			name: "первый_запрос_не_сдвигает_сетку_окон",
			steps: []step{
				{at: 200 * time.Millisecond, requests: 4, allowed: 4},
				{at: 1250 * time.Millisecond, requests: 2, allowed: 1},
			},
		},
		{
			name: "погрешность_недооценка_всплеска_в_конце_окна",
			steps: []step{
				{at: 900 * time.Millisecond, requests: 4, allowed: 4},
				{at: 1500 * time.Millisecond, requests: 3, allowed: 2},
				// В реальной последней секунде уже 6 принятых запросов.
				// Counter — приближение; строгую квоту sliding log он не обещает.
			},
		},
		{
			name: "погрешность_переоценка_всплеска_в_начале_окна",
			steps: []step{
				{at: 0, requests: 4, allowed: 4},
				{at: 1100 * time.Millisecond, requests: 1, allowed: 0},
				// Реальные запросы уже старше секунды, но оценка ещё равна 3.6.
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := New(4, time.Second)
			start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

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
		name   string
		limit  int
		window time.Duration
	}{
		{name: "нулевой_лимит", limit: 0, window: time.Second},
		{name: "отрицательный_лимит", limit: -1, window: time.Second},
		{name: "нулевое_окно", limit: 1, window: 0},
		{name: "отрицательное_окно", limit: 1, window: -time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("ожидается panic для ошибочной конфигурации")
				}
			}()

			New(tt.limit, tt.window)
		})
	}
}
