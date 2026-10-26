package requesttimeout

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"
)

func TestHTTPBudget(t *testing.T) {
	tests := []struct {
		name   string
		header string
		parent time.Duration
		want   time.Duration
	}{
		{name: "default", want: 5 * time.Second},
		{name: "shorter client budget", header: "2s", want: 2 * time.Second},
		{name: "cannot extend budget", header: "1m", want: 5 * time.Second},
		{name: "invalid", header: "bad", want: 5 * time.Second},
		{name: "zero", header: "0s", want: 5 * time.Second},
		{name: "negative", header: "-1s", want: 5 * time.Second},
		{name: "earlier parent deadline", header: "2s", parent: time.Second, want: time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx := t.Context()
				if test.parent > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, test.parent)
					defer cancel()
				}
				request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/echo", nil)
				request.Header.Set("X-Request-Timeout", test.header)
				start := time.Now()
				handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					<-r.Context().Done()
					if r.Context().Err() != context.DeadlineExceeded {
						t.Fatalf("context error = %v", r.Context().Err())
					}
				}))
				handler.ServeHTTP(httptest.NewRecorder(), request)
				if elapsed := time.Since(start); elapsed != test.want {
					t.Fatalf("budget = %v, want %v", elapsed, test.want)
				}
			})
		})
	}
}

func TestGRPCBudget(t *testing.T) {
	for _, parent := range []time.Duration{0, 2 * time.Second, 10 * time.Second} {
		t.Run(parent.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx := t.Context()
				if parent > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, parent)
					defer cancel()
				}
				start := time.Now()
				_, err := Interceptor()(ctx, nil, nil, func(ctx context.Context, _ any) (any, error) {
					<-ctx.Done()
					return nil, ctx.Err()
				})
				want := 5 * time.Second
				if parent > 0 && parent < want {
					want = parent
				}
				if err != context.DeadlineExceeded || time.Since(start) != want {
					t.Fatalf("elapsed=%v, error=%v; want deadline after %v", time.Since(start), err, want)
				}
			})
		})
	}
}

func TestHTTPBudgetIsNotRestartedAtRPCHandler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Time spent before entering the RPC handler belongs to the same budget.
			time.Sleep(2 * time.Second)
			_, err := Interceptor()(r.Context(), nil, nil, func(ctx context.Context, _ any) (any, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			})
			if err != context.DeadlineExceeded {
				t.Fatalf("error = %v", err)
			}
		}))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/echo", nil))
		if elapsed := time.Since(start); elapsed != 5*time.Second {
			t.Fatalf("total processing time = %v, want 5s", elapsed)
		}
	})
}
