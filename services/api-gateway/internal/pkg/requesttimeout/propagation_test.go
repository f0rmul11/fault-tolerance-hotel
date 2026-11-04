package requesttimeout

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestHTTPPropagationSpendsOneBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		terminal := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
			http.Error(w, "Время ожидания истекло", http.StatusGatewayTimeout)
		}))
		transport := Transport{Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if got := r.Header.Get(Header); got != "3s" {
				t.Fatalf("remaining budget = %q, want 3s", got)
			}
			// На другом сервере новый контекст: deadline должен прийти именно из заголовка.
			remote := r.Clone(context.Background())
			recorder := httptest.NewRecorder()
			terminal.ServeHTTP(recorder, remote)
			return recorder.Result(), nil
		})}
		entry := Limit(MaxDuration)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second) // Локальная работа; synctest использует виртуальное время.
			outgoing := httptest.NewRequestWithContext(r.Context(), http.MethodGet, "http://review/ratings", nil)
			outgoing.Header.Set(Header, "5s")
			response, err := transport.RoundTrip(outgoing)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if outgoing.Header.Get(Header) != "5s" {
				t.Fatal("transport changed original headers")
			}
			if response.StatusCode != http.StatusGatewayTimeout {
				t.Fatalf("status = %d", response.StatusCode)
			}
		}))
		entry.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		if elapsed := time.Since(started); elapsed != 5*time.Second {
			t.Fatalf("total = %s, want 5s", elapsed)
		}
	})
}

func TestTransportWithoutDeadlineRemovesStaleHeader(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://review/ratings", nil)
	request.Header.Set(Header, "5s")
	transport := Transport{Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get(Header) != "" {
			t.Fatal("stale budget propagated without a context deadline")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if request.Header.Get(Header) != "5s" {
		t.Fatal("original headers changed")
	}
}

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error {
	b.closed = true
	return nil
}

func TestTransportRejectsExpiredContext(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "http://review/ratings", nil)
	body := &trackedBody{Reader: strings.NewReader("{}")}
	request.Body = body
	transport := Transport{Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("expired request reached network")
		return nil, nil
	})}
	_, err := transport.RoundTrip(request)
	if !errors.Is(err, context.DeadlineExceeded) || !body.closed {
		t.Fatalf("error = %v, body closed = %v", err, body.closed)
	}
}

func TestHTTPPropagationCancellationOverNetwork(t *testing.T) {
	started := make(chan time.Duration, 1)
	stopped := make(chan error, 1)
	server := httptest.NewServer(Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, _ := r.Context().Deadline()
		started <- time.Until(deadline)
		<-r.Context().Done()
		stopped <- r.Context().Err()
	})))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	base := http.DefaultTransport.(*http.Transport).Clone()
	defer base.CloseIdleConnections()
	client := &http.Client{Transport: Transport{Base: base}}
	result := make(chan error, 1)
	go func() {
		response, err := client.Do(request)
		if response != nil {
			response.Body.Close()
		}
		result <- err
	}()
	select {
	case remaining := <-started:
		if remaining <= 0 || remaining > 2*time.Second {
			t.Fatalf("server budget = %s, want at most 2s", remaining)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not receive request")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("client error = %v", err)
	}
	select {
	case err := <-stopped:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("server error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not observe cancellation")
	}
}
