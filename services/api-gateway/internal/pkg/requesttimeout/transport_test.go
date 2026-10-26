package requesttimeout_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"api-gateway/internal/pkg/grpc/intercept"
	"api-gateway/internal/pkg/httptransport"
	pb "api-gateway/internal/pkg/pb/api-gateway/echo/v1"
	"api-gateway/internal/pkg/requestid"
	"api-gateway/internal/pkg/requesttimeout"

	grpcmiddleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/not-for-prod/clay/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestHTTPDeadlineCancelsDownstreamAndReturns504(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	backend := &waitingEcho{
		canceled: make(chan error, 1),
	}
	server := grpc.NewServer()
	pb.RegisterEchoServiceServer(server, backend)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///timeout-test",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithUnaryInterceptor(intercept.ProxyMetadata()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})

	descriptor := pb.NewEchoServiceServiceDesc(&echoProxy{client: pb.NewEchoServiceClient(conn)})
	descriptor.Apply(transport.WithUnaryInterceptor(grpcmiddleware.ChainUnaryServer(
		requesttimeout.Interceptor(),
		requestid.Interceptor(),
	)))
	mux := runtime.NewServeMux(httptransport.Options()...)
	if err := descriptor.RegisterHTTP(t.Context(), mux); err != nil {
		t.Fatal(err)
	}
	handler := requesttimeout.Middleware(mux)
	const requestID = "1b085786-a2db-43aa-8387-4e67cd626647"
	for _, message := range []string{"fast", "slow"} {
		request := httptest.NewRequest(http.MethodGet, "/v1/echo?message="+message, nil)
		request.Header.Set("X-Request-Timeout", "200ms")
		// A second client timeout mechanism cannot override the gateway's budget.
		request.Header.Set("Grpc-Timeout", "60S")
		request.Header.Set("X-Request-Id", requestID)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := http.StatusOK
		if message == "slow" {
			want = http.StatusGatewayTimeout
		}
		if response.Code != want || response.Header().Get("X-Request-Id") != requestID {
			t.Fatalf("%s: status=%d, headers=%v, body=%s", message, response.Code, response.Header(), response.Body.String())
		}
		if message == "slow" {
			var body struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != int(codes.DeadlineExceeded) || body.Message != "Время ожидания ответа сервиса истекло" {
				t.Fatalf("unexpected timeout response: %+v", body)
			}
		}
	}
	select {
	case err := <-backend.canceled:
		if err != context.DeadlineExceeded && err != context.Canceled {
			t.Fatalf("downstream cancellation = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("downstream did not receive cancellation")
	}
}

type echoProxy struct {
	pb.UnimplementedEchoServiceServer
	client pb.EchoServiceClient
}

func (p *echoProxy) Echo(ctx context.Context, request *pb.EchoRequest) (*pb.EchoResponse, error) {
	return p.client.Echo(ctx, request)
}

type waitingEcho struct {
	pb.UnimplementedEchoServiceServer
	canceled chan error
}

func (s *waitingEcho) Echo(ctx context.Context, request *pb.EchoRequest) (*pb.EchoResponse, error) {
	if request.GetMessage() != "slow" {
		return &pb.EchoResponse{Message: request.GetMessage()}, nil
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 200*time.Millisecond {
		return nil, status.Error(codes.Internal, "downstream did not receive the remaining budget")
	}
	<-ctx.Done()
	s.canceled <- ctx.Err()
	return nil, status.FromContextError(ctx.Err()).Err()
}
