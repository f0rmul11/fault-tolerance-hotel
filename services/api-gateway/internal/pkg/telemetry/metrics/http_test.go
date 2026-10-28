package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pb "api-gateway/internal/pkg/pb/api-gateway/echo/v1"
	"github.com/go-chi/chi/v5"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestHTTPCountsLocalAdapterOnceIncludingInvalidJSON(t *testing.T) {
	isolatedMetrics(t)
	mux := runtime.NewServeMux(runtime.WithMiddlewares(AnnotateHTTPRoute), runtime.WithErrorHandler(func(ctx context.Context, mux *runtime.ServeMux, m runtime.Marshaler, w http.ResponseWriter, r *http.Request, err error) {
		code := runtime.HTTPStatusFromCode(status.Code(err))
		if status.Code(err) == codes.InvalidArgument {
			code = 422
		}
		runtime.DefaultHTTPErrorHandler(ctx, mux, m, w, r, &runtime.HTTPStatusError{HTTPStatus: code, Err: err})
	}))
	if err := pb.NewEchoServiceServiceDesc(echoEndpoint{}).RegisterHTTP(context.Background(), mux); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	router.Use(HTTP)
	router.Mount("/", mux)
	for _, tc := range []struct {
		body, code, category string
		status               int
	}{
		{`{"message":"ok"}`, "200", "ok", 200},
		{`{"message":`, "422", "client_error", 422},
		{`{"message":"14"}`, "503", "error", 503},
	} {
		r := httptest.NewRequest("POST", "/v1/echo?irrelevant=unique-id", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if got := testutil.ToFloat64(serverRequests.WithLabelValues("http", "POST /v1/echo", tc.category, tc.code)); got != 1 {
			t.Fatalf("count=%v for%s", got, tc.code)
		}
	}
	registry := prometheus.NewRegistry()
	registry.MustRegister(serverRequests)
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range families[0].Metric {
		for _, label := range metric.Label {
			if label.GetName() == "protocol" && label.GetValue() == "grpc" {
				t.Fatal("local HTTP adapter counted as a network gRPC request")
			}
		}
	}
}

func TestHTTPRoutingLabelsAndFinalStatus(t *testing.T) {
	isolatedMetrics(t)
	h := HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/empty" {
			return
		}
		if r.URL.Path == "/early" {
			w.WriteHeader(103)
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(404)
	}))
	for _, path := range []string{"/id/123", "/id/456", "/healthcheck/ready", "/early", "/empty"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	for _, tc := range []struct {
		category, status string
		want             float64
	}{{"not_found", "404", 2}, {"error", "503", 1}, {"ok", "200", 1}} {
		if got := testutil.ToFloat64(serverRequests.WithLabelValues("http", "GET unmatched", tc.category, tc.status)); got != tc.want {
			t.Fatalf("%s count=%v want%v", tc.status, got, tc.want)
		}
	}
}
