package httptransport_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"api-gateway/internal/pkg/grpc/intercept"
	"api-gateway/internal/pkg/httptransport"
	pb "api-gateway/internal/pkg/pb/api-gateway/echo/v1"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// Exercise actual gRPC metadata -> ProxyMetadata -> generated Clay HTTP adapter.
func TestRateLimitHeadersSurviveProxyOnSuccessAndRejection(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	pb.RegisterEchoServiceServer(server, &quotaEcho{})
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///quota-test",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithChainUnaryInterceptor(intercept.ProxyMetadata(), intercept.ClientName("api-gateway")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})
	mux := runtime.NewServeMux(httptransport.Options()...)
	descriptor := pb.NewEchoServiceServiceDesc(&quotaProxy{client: pb.NewEchoServiceClient(conn)})
	if err := descriptor.RegisterHTTP(t.Context(), mux); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		message string
		status  int
		retry   string
	}{
		{message: "allowed", status: http.StatusOK},
		{message: "rejected", status: http.StatusTooManyRequests, retry: "1"},
	} {
		t.Run(test.message, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/v1/echo?message="+test.message, nil)
			request.Header.Set("X-Client-Name", "spoofed")
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			expected := map[string]string{
				"X-RateLimit-Limit":     "10",
				"X-RateLimit-Burst":     "200",
				"X-RateLimit-Remaining": "0",
				"X-RateLimit-Reset":     "1800000000",
				"Retry-After":           test.retry,
			}
			for key, value := range expected {
				if response.Header().Get(key) != value {
					t.Fatalf("%s=%q want=%q; all=%v", key, response.Header().Get(key), value, response.Header())
				}
			}
		})
	}
}

type quotaEcho struct {
	pb.UnimplementedEchoServiceServer
}

func (*quotaEcho) Echo(ctx context.Context, request *pb.EchoRequest) (*pb.EchoResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if names := md.Get("x-client-name"); len(names) != 1 || names[0] != "api-gateway" {
		return nil, status.Error(codes.Internal, "wrong caller identity")
	}
	headers := metadata.Pairs("x-ratelimit-limit", "10", "x-ratelimit-burst", "200", "x-ratelimit-remaining", "0", "x-ratelimit-reset", "1800000000")
	if request.GetMessage() == "rejected" {
		headers.Set("retry-after", "1")
	}
	_ = grpc.SetHeader(ctx, headers)
	if request.GetMessage() == "rejected" {
		return nil, status.Error(codes.ResourceExhausted, "Превышен лимит запросов к методу сервиса")
	}
	return &pb.EchoResponse{Message: request.GetMessage()}, nil
}

type quotaProxy struct {
	pb.UnimplementedEchoServiceServer
	client pb.EchoServiceClient
}

func (p *quotaProxy) Echo(ctx context.Context, request *pb.EchoRequest) (*pb.EchoResponse, error) {
	return p.client.Echo(ctx, request)
}
