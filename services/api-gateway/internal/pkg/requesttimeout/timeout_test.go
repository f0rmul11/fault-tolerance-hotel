package requesttimeout

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"
)

func TestHTTPPropagationBudget(t *testing.T) {
	tests := []struct {
		name    string
		values  []string
		parent  time.Duration
		want    time.Duration
		invalid bool
	}{
		{name: "absent"},
		{name: "received budget", values: []string{"2s"}, want: 2 * time.Second},
		{name: "not capped at gateway limit", values: []string{"20s"}, want: 20 * time.Second},
		{name: "zero expires immediately", values: []string{"0s"}},
		{name: "earlier parent", values: []string{"20s"}, parent: time.Second, want: time.Second},
		{name: "parent without header", parent: time.Second, want: time.Second},
		{name: "malformed", values: []string{"bad"}, invalid: true},
		{name: "empty", values: []string{""}, invalid: true},
		{name: "negative", values: []string{"-1s"}, invalid: true},
		{name: "overflow", values: []string{"999999999999999999999h"}, invalid: true},
		{name: "duplicate", values: []string{"1s", "2s"}, invalid: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx := context.Background()
				if test.parent > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, test.parent)
					defer cancel()
				}
				request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
				for _, value := range test.values {
					request.Header.Add(Header, value)
				}
				called := false
				handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					deadline, ok := r.Context().Deadline()
					if test.values == nil && test.parent == 0 {
						if ok {
							t.Fatal("absent header added deadline")
						}
						return
					}
					if !ok || time.Until(deadline) != test.want {
						t.Fatalf("deadline present=%v, remaining=%s, want %s", ok, time.Until(deadline), test.want)
					}
					if test.want == 0 && r.Context().Err() != context.DeadlineExceeded {
						t.Fatal("zero budget was not expired")
					}
				}))
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if test.invalid {
					if called || response.Code != http.StatusBadRequest {
						t.Fatalf("invalid header: handler called=%v, status=%d", called, response.Code)
					}
				} else if !called {
					t.Fatal("valid request rejected")
				}
			})
		})
	}
}

func TestHTTPEntryLimit(t *testing.T) {
	for _, header := range []string{"", "2s", "20s"} {
		t.Run(header, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				want := MaxDuration
				if header == "2s" {
					want = 2 * time.Second
				}
				handler := Limit(MaxDuration)(Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					deadline, ok := r.Context().Deadline()
					if !ok || time.Until(deadline) != want {
						t.Fatalf("entry budget = %s, want %s", time.Until(deadline), want)
					}
				})))
				request := httptest.NewRequest(http.MethodGet, "/", nil)
				if header != "" {
					request.Header.Set(Header, header)
				}
				handler.ServeHTTP(httptest.NewRecorder(), request)
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
		handler := Limit(MaxDuration)(Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Time spent before entering the RPC handler belongs to the same budget.
			time.Sleep(2 * time.Second)
			_, err := Interceptor()(r.Context(), nil, nil, func(ctx context.Context, _ any) (any, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			})
			if err != context.DeadlineExceeded {
				t.Fatalf("error = %v", err)
			}
		})))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/echo", nil))
		if elapsed := time.Since(start); elapsed != 5*time.Second {
			t.Fatalf("total processing time = %v, want 5s", elapsed)
		}
	})
}
