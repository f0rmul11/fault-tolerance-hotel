package traces_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"api-gateway/internal/pkg/httptransport"
	pb "api-gateway/internal/pkg/pb/api-gateway/echo/v1"
	"api-gateway/internal/pkg/telemetry/traces"
)

func traceProviderForTest(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return recorder
}

// Clay registers a local generated HTTP adapter, not another network gRPC server.
// This exercises that adapter plus a real in-process downstream gRPC exchange.
func TestClayHTTPAndGRPCShareTraceWithoutDuplicateServerSpan(t *testing.T) {
	recorder := traceProviderForTest(t)
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.StatsHandler(traces.ServerHandler()))
	pb.RegisterEchoServiceServer(server, &tracedEchoEndpoint{})
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	conn, err := grpc.NewClient("passthrough:///trace-test",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithStatsHandler(traces.ClientHandler()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})

	mux := runtime.NewServeMux(httptransport.Options()...)
	descriptor := pb.NewEchoServiceServiceDesc(&tracedEchoProxy{client: pb.NewEchoServiceClient(conn)})
	if err := descriptor.RegisterHTTP(context.Background(), mux); err != nil {
		t.Fatal(err)
	}
	incoming := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{2},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/v1/echo?message=hello", nil).WithContext(ctx)
	request.Header.Set("traceparent", "00-"+incoming.TraceID().String()+"-"+incoming.SpanID().String()+"-01")
	response := httptest.NewRecorder()
	router := chi.NewMux()
	router.Use(traces.HTTP)
	router.Mount("/", mux)
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Message != "hello" {
		t.Fatalf("HTTP adapter did not return downstream response: %s, %v", response.Body.String(), err)
	}

	// The network server's RPC-end hook can run immediately after the client gets
	// its response. Wait for that hook before inspecting the complete span tree.
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for len(recorder.Ended()) < 5 {
		select {
		case <-deadline.C:
			t.Fatalf("only %d spans ended", len(recorder.Ended()))
		case <-tick.C:
		}
	}
	spans := recorder.Ended()
	if len(spans) != 5 {
		t.Fatalf("got %d spans, want HTTP + local handler + gRPC client/server + downstream handler", len(spans))
	}
	byName := make(map[string]sdktrace.ReadOnlySpan)
	var client, downstream sdktrace.ReadOnlySpan
	serverCount := 0
	for _, span := range spans {
		if span.SpanContext().TraceID() != incoming.TraceID() {
			t.Errorf("span %q started another trace", span.Name())
		}
		byName[span.Name()] = span
		if span.SpanKind() == trace.SpanKindClient {
			client = span
		}
		if span.SpanKind() == trace.SpanKindServer {
			serverCount++
			if span.Name() != "GET /v1/echo" {
				downstream = span
			}
		}
	}
	httpSpan := byName["GET /v1/echo"]
	localHandler := byName["handler.localEcho"]
	remoteHandler := byName["handler.downstreamEcho"]
	if httpSpan == nil || localHandler == nil || remoteHandler == nil || client == nil || downstream == nil || serverCount != 2 {
		t.Fatalf("unexpected transport spans: %+v", spans)
	}
	if httpSpan.Parent().SpanID() != incoming.SpanID() ||
		localHandler.Parent().SpanID() != httpSpan.SpanContext().SpanID() ||
		client.Parent().SpanID() != localHandler.SpanContext().SpanID() ||
		downstream.Parent().SpanID() != client.SpanContext().SpanID() ||
		remoteHandler.Parent().SpanID() != downstream.SpanContext().SpanID() {
		t.Fatal("HTTP/local/gRPC layers lost their parent-child chain")
	}
	if !downstream.Parent().IsRemote() {
		t.Fatal("gRPC server must extract its remote parent from metadata")
	}
	route := ""
	for _, attribute := range httpSpan.Attributes() {
		if string(attribute.Key) == "http.route" {
			route = attribute.Value.AsString()
		}
	}
	if route != "/v1/echo" {
		t.Fatalf("HTTP route = %q, want matched route template", route)
	}
}

func TestHTTPProbeAndDebugRequestsAreNotTraced(t *testing.T) {
	recorder := traceProviderForTest(t)
	calls := 0
	handler := traces.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, path := range []string{"/healthcheck/ready", "/debug/pprof/", "/swagger.json"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	if calls != 3 || len(recorder.Ended()) != 0 {
		t.Fatalf("filters must preserve handlers without recording spans: calls=%d spans=%d", calls, len(recorder.Ended()))
	}
}

type tracedEchoProxy struct {
	pb.UnimplementedEchoServiceServer
	client pb.EchoServiceClient
}

func (p *tracedEchoProxy) Echo(ctx context.Context, request *pb.EchoRequest) (*pb.EchoResponse, error) {
	ctx, span := traces.Start(ctx, "handler.localEcho")
	defer span.End()
	return p.client.Echo(ctx, request)
}

type tracedEchoEndpoint struct {
	pb.UnimplementedEchoServiceServer
}

func (s *tracedEchoEndpoint) Echo(ctx context.Context, request *pb.EchoRequest) (*pb.EchoResponse, error) {
	_, span := traces.Start(ctx, "handler.downstreamEcho")
	defer span.End()
	return &pb.EchoResponse{Message: request.GetMessage()}, nil
}

func TestHTTPRouteNamesRemainIsolatedAcrossConcurrentRequests(t *testing.T) {
	recorder := traceProviderForTest(t)
	mux := runtime.NewServeMux(httptransport.Options()...)
	patterns := []string{"/v1/hotels/{id=*}", "/v1/rooms/{id=*}"}
	for _, pattern := range patterns {
		if err := mux.HandlePath(http.MethodGet, pattern, func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
			trace.SpanFromContext(r.Context()).SetAttributes(attribute.String("test.expected_route", pattern))
			w.WriteHeader(http.StatusNoContent)
		}); err != nil {
			t.Fatal(err)
		}
	}
	router := chi.NewMux()
	router.Use(traces.HTTP)
	router.Mount("/", mux)

	const requestCount = 64
	var requests sync.WaitGroup
	for i := 0; i < requestCount; i++ {
		requests.Add(1)
		go func() {
			defer requests.Done()
			path := "/v1/hotels/" + strconv.Itoa(i)
			if i%2 != 0 {
				path = "/v1/rooms/" + strconv.Itoa(i)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusNoContent {
				t.Errorf("HTTP status = %d for %s", response.Code, path)
			}
		}()
	}
	requests.Wait()

	spans := recorder.Ended()
	if len(spans) != requestCount {
		t.Fatalf("ended spans = %d, want %d", len(spans), requestCount)
	}
	for _, span := range spans {
		expectedRoute := ""
		actualRoute := ""
		for _, attr := range span.Attributes() {
			switch string(attr.Key) {
			case "test.expected_route":
				expectedRoute = attr.Value.AsString()
			case "http.route":
				actualRoute = attr.Value.AsString()
			}
		}
		if expectedRoute == "" || actualRoute != expectedRoute || span.Name() != http.MethodGet+" "+expectedRoute {
			t.Errorf("request route leaked or was overwritten: name=%q route=%q want=%q", span.Name(), actualRoute, expectedRoute)
		}
	}
}
