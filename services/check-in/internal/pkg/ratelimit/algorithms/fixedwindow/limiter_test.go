package fixedwindow

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
			name: "лимит_и_точная_граница_окна",
			steps: []step{
				{at: 0, requests: 4, allowed: 3},
				{at: time.Second - time.Nanosecond, requests: 1, allowed: 0},
				{at: time.Second, requests: 4, allowed: 3},
			},
		},
		{
			name: "двойной_burst_на_стыке",
			steps: []step{
				{at: 999 * time.Millisecond, requests: 3, allowed: 3},
				{at: time.Second, requests: 3, allowed: 3}, // 6 запросов за 1 мс при лимите 3/с.
			},
		},
		{
			name: "окно_не_сдвигается_по_первому_запросу",
			steps: []step{
				{at: 200 * time.Millisecond, requests: 3, allowed: 3},
				{at: time.Second, requests: 3, allowed: 3}, // Сброс в 1 с, не в 1.2 с.
			},
		},
		{
			name: "простой_и_отказы_не_съедают_следующее_окно",
			steps: []step{
				{at: 0, requests: 100, allowed: 3},
				{at: time.Hour, requests: 4, allowed: 3},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := New(3, time.Second)
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
