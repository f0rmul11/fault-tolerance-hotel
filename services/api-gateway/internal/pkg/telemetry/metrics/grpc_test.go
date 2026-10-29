package metrics

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	pb "api-gateway/internal/pkg/pb/api-gateway/echo/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"
	"google.golang.org/grpc/test/bufconn"
)

func isolatedMetrics(t *testing.T) {
	t.Helper()
	previousRequests, previousDuration := serverRequests, serverDuration
	serverRequests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_requests", Help: "test"}, []string{"protocol", "handler", "code", "status"})
	serverDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "test_duration", Help: "test"}, []string{"protocol", "handler", "code"})
	t.Cleanup(func() { serverRequests, serverDuration = previousRequests, previousDuration })
}

type observedStats struct {
	stats.Handler
	ended chan context.Context
}

func (s observedStats) HandleRPC(ctx context.Context, event stats.RPCStats) {
	s.Handler.HandleRPC(ctx, event)
	if _, ok := event.(*stats.End); ok {
		s.ended <- ctx
	}
}

type echoEndpoint struct {
	pb.UnimplementedEchoServiceServer
}

func (echoEndpoint) Echo(_ context.Context, request *pb.EchoRequest) (*pb.EchoResponse, error) {
	if request.Message == "mapped" {
		return nil, errors.New("business conflict")
	}
	n, _ := strconv.Atoi(request.Message)
	if n != 0 {
		return nil, status.Error(codes.Code(n), "test response")
	}
	return &pb.EchoResponse{Message: request.Message}, nil
}

type malformedCodec struct{}

func (malformedCodec) Name() string                { return "proto" }
func (malformedCodec) Marshal(any) ([]byte, error) { return []byte{0xff}, nil }
func (malformedCodec) Unmarshal([]byte, any) error { return nil }

func TestGRPCCountsFinalStatusIncludingInterceptorAndDecodeFailures(t *testing.T) {
	isolatedMetrics(t)
	listener := bufconn.Listen(1024 * 1024)
	observer := observedStats{Handler: ServerHandler(), ended: make(chan context.Context, 32)}
	server := grpc.NewServer(grpc.InTapHandle(TapHandler), grpc.StatsHandler(observer),
		grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
			if req.(*pb.EchoRequest).Message == "auth" {
				return nil, status.Error(codes.PermissionDenied, "denied")
			}
			response, err := next(ctx, req)
			if err != nil && status.Code(err) == codes.Unknown {
				return nil, status.Error(codes.Aborted, "mapped conflict")
			}
			return response, err
		}))
	pb.RegisterEchoServiceServer(server, echoEndpoint{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///metrics-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := pb.NewEchoServiceClient(conn)
	cases := []struct {
		message   string
		code      codes.Code
		category  string
		malformed bool
	}{
		{"ok", codes.OK, "ok", false},
		{"auth", codes.PermissionDenied, "client_error", false},
		{"mapped", codes.Aborted, "client_error", false},
		{"14", codes.Unavailable, "error", false},
		{"4", codes.DeadlineExceeded, "error", false},
		{"5", codes.NotFound, "not_found", false},
		{"1", codes.Canceled, "canceled", false},
		{"broken protobuf", codes.Internal, "error", true},
	}
	for _, tc := range cases {
		t.Run(tc.message, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			defer cancel()
			var options []grpc.CallOption
			if tc.malformed {
				options = append(options, grpc.ForceCodec(malformedCodec{}))
			}
			_, err := client.Echo(ctx, &pb.EchoRequest{Message: tc.message}, options...)
			if status.Code(err) != tc.code {
				t.Fatalf("status=%v want %v", err, tc.code)
			}
			select {
			case ended := <-observer.ended:
				method, ok := ended.Value(rpcMethodKey{}).(string)
				if !ok || method == "unknown" {
					t.Fatalf("lost method: %v", method)
				}
				if _, ok := ended.Value(rpcStartKey{}).(time.Time); !ok {
					t.Fatal("tap start was lost")
				}
				if got := testutil.ToFloat64(serverRequests.WithLabelValues("grpc", method, tc.category, tc.code.String())); got != 1 {
					t.Fatalf("count=%v want1", got)
				}
			case <-ctx.Done():
				t.Fatal("missing stats.End")
			}
		})
	}
	server.Stop()
}

func TestDurationStartsAtTapAndProbesAreExcluded(t *testing.T) {
	isolatedMetrics(t)
	h := ServerHandler()
	ctx, err := TapHandler(context.Background(), &tap.Info{})
	if err != nil {
		t.Fatal(err)
	}
	started := ctx.Value(rpcStartKey{}).(time.Time)
	ctx = h.TagRPC(ctx, &stats.RPCTagInfo{FullMethodName: "/unknown/request-123"})
	h.HandleRPC(ctx, &stats.End{BeginTime: started.Add(2 * time.Second), EndTime: started.Add(5 * time.Second)})
	var metric dto.Metric
	if err := serverDuration.WithLabelValues("grpc", "unknown", "ok").(prometheus.Metric).Write(&metric); err != nil {
		t.Fatal(err)
	}
	if got := metric.GetHistogram().GetSampleSum(); got != 5 {
		t.Fatalf("duration=%v want tap-to-End=5, not Begin-to-End=3", got)
	}
	for _, method := range []string{"/grpc.health.v1.Health/Check", "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo"} {
		probe := h.TagRPC(context.Background(), &stats.RPCTagInfo{FullMethodName: method})
		if probe.Value(rpcMethodKey{}) != nil {
			t.Fatalf("probe %s counted", method)
		}
	}
	if knownMethod("/unknown/request-456") != "unknown" {
		t.Fatal("unbounded method label")
	}
}
