package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"check-in/internal/pkg/grpc/clientname"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestRuleMatching(t *testing.T) {
	tests := []struct {
		name   string
		rules  []MainConfig
		client string
		method string
		want   codes.Code
	}{
		{name: "правила выключены", want: codes.OK},
		{name: "wildcard запрещает всё", rules: []MainConfig{{Limit: 0, Burst: 100}}, want: codes.ResourceExhausted},
		{name: "другой клиент", rules: []MainConfig{{Clients: []string{"api-gateway"}}}, client: "unknown", want: codes.OK},
		{name: "другой метод", rules: []MainConfig{{Handlers: []string{"/test/Read"}}}, method: "/test/Write", want: codes.OK},
		{name: "оба условия совпали", rules: []MainConfig{{Clients: []string{"api-gateway"}, Handlers: []string{"/test/Read"}}}, client: "api-gateway", method: "/test/Read", want: codes.ResourceExhausted},
		{name: "первое совпадение выигрывает", rules: []MainConfig{{Limit: 0}, {Limit: 1, Burst: 1}}, want: codes.ResourceExhausted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := NewLimiter(Config{Rules: tt.rules})
			called := false
			_, err := limiter.UnaryServerInterceptor()(clientname.NewContext(t.Context(), tt.client), nil, &grpc.UnaryServerInfo{FullMethod: tt.method}, func(context.Context, any) (any, error) {
				called = true
				return "response", nil
			})
			if status.Code(err) != tt.want || called != (tt.want == codes.OK) {
				t.Fatalf("код=%v handler=%v, ожидался %v", status.Code(err), called, tt.want)
			}
		})
	}
}

// Проверяем реальную выдачу metadata interceptor, а не константы в заглушке Gateway.
func TestBurstHeadersAndMetrics(t *testing.T) {
	limiter := NewLimiter(Config{Rules: []MainConfig{{Limit: 0.001, Burst: 2}}})
	registry := prometheus.NewRegistry()
	if err := limiter.RegisterMetrics(registry); err != nil {
		t.Fatal(err)
	}
	for i, want := range []codes.Code{codes.OK, codes.OK, codes.ResourceExhausted} {
		stream := &headerStream{}
		ctx := grpc.NewContextWithServerTransportStream(t.Context(), stream)
		response, err := limiter.UnaryServerInterceptor()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test/Read"}, func(context.Context, any) (any, error) {
			return "response", nil
		})
		if status.Code(err) != want {
			t.Fatalf("запрос %d: %v", i, err)
		}
		if want == codes.OK && response != "response" {
			t.Fatalf("ответ handler потерян: %v", response)
		}
		for _, key := range []string{"x-ratelimit-limit", "x-ratelimit-burst", "x-ratelimit-remaining", "x-ratelimit-reset"} {
			if len(stream.headers.Get(key)) != 1 {
				t.Fatalf("запрос %d: отсутствует %s: %v", i, key, stream.headers)
			}
		}
		if (len(stream.headers.Get("retry-after")) > 0) != (want == codes.ResourceExhausted) {
			t.Fatalf("Retry-After: %v", stream.headers)
		}
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	outcomes := map[string]float64{}
	for _, family := range families {
		if family.GetName() != "ratelimit_decisions_total" {
			continue
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetName() == "outcome" {
					outcomes[label.GetValue()] = metric.GetCounter().GetValue()
				}
			}
		}
	}
	if outcomes["allowed"] != 2 || outcomes["rejected"] != 1 || outcomes["canceled"] != 0 {
		t.Fatalf("неверные метрики решений: %v", outcomes)
	}
}

func TestConcurrentBurst(t *testing.T) {
	limiter := NewLimiter(Config{Rules: []MainConfig{{Limit: 0.000001, Burst: 7}}})
	var admitted atomic.Int64
	var rejected atomic.Int64
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			err := limiter.doIfAllowed(t.Context(), "/test/Read", func() error {
				admitted.Add(1)
				return nil
			})
			if status.Code(err) == codes.ResourceExhausted {
				rejected.Add(1)
			} else if err != nil {
				t.Errorf("неожиданная ошибка: %v", err)
			}
		})
	}
	wg.Wait()
	if admitted.Load() != 7 || rejected.Load() != 93 {
		t.Fatalf("допущено=%d отказов=%d", admitted.Load(), rejected.Load())
	}
}

func TestWaitAndCancellation(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		deadline time.Duration
		cancel   bool
		want     codes.Code
	}{
		{name: "ожидание пополнения", timeout: 2 * time.Second, want: codes.OK},
		{name: "токен не успеет в локальный бюджет", timeout: 100 * time.Millisecond, want: codes.ResourceExhausted},
		{name: "истёк входящий deadline", timeout: time.Second, deadline: -time.Second, want: codes.DeadlineExceeded},
		{name: "отмена до ожидания", timeout: time.Second, cancel: true, want: codes.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				limiter := NewLimiter(Config{Rules: []MainConfig{{Limit: 1, Burst: 1, Timeout: tt.timeout}}})
				handler := func() error {
					return nil
				}
				if err := limiter.doIfAllowed(t.Context(), "/test/Read", handler); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if tt.cancel {
					cancel()
				}
				if tt.deadline != 0 {
					var stop context.CancelFunc
					ctx, stop = context.WithTimeout(ctx, tt.deadline)
					defer stop()
				}
				err := limiter.doIfAllowed(ctx, "/test/Read", handler)
				if status.Code(err) != tt.want {
					t.Fatalf("получен %v, ожидался %v", err, tt.want)
				}
			})
		})
	}
}

type headerStream struct{ headers metadata.MD }

func (*headerStream) Method() string {
	return "/test/Read"
}
func (s *headerStream) SetHeader(md metadata.MD) error {
	s.headers = metadata.Join(s.headers, md)
	return nil
}
func (s *headerStream) SendHeader(md metadata.MD) error {
	return s.SetHeader(md)
}
func (*headerStream) SetTrailer(metadata.MD) error { return nil }
