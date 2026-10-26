package kube

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/fault"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Run in an isolated environment: the rendered listeners use ports 9901 and 10001–11005.
func TestEnvoyTransport(t *testing.T) {
	binary := os.Getenv("ENVOY_BINARY")
	if binary == "" {
		t.Skip("set ENVOY_BINARY to run the real Envoy transport test")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	const traceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	backend := grpc.NewServer()
	backend.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.Booking",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Create",
			Handler: func(_ any, ctx context.Context, decode func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
				var request wrapperspb.StringValue
				if err := decode(&request); err != nil {
					return nil, err
				}
				calls.Add(1)
				md, _ := metadata.FromIncomingContext(ctx)
				if strings.Join(md.Get("traceparent"), "") != traceparent || strings.Join(md.Get("x-demo-actor"), "") != "guest-42" {
					return nil, status.Error(codes.Internal, "request metadata was lost")
				}
				return &request, nil
			},
		}},
	}, struct{}{})
	go backend.Serve(listener)
	t.Cleanup(backend.Stop)

	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	objects, err := RenderEnvoy(config.Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	var bootstrap map[string]any
	for _, object := range objects {
		if object.GetKind() == "ConfigMap" && object.GetName() == "envoy-config" {
			content, _, err := unstructured.NestedString(object.Object, "data", "envoy.json")
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(content), &bootstrap); err != nil {
				t.Fatal(err)
			}
		}
	}
	if bootstrap == nil {
		t.Fatal("rendered Envoy ConfigMap missing")
	}
	for _, raw := range bootstrap["static_resources"].(map[string]any)["clusters"].([]any) {
		cluster := raw.(map[string]any)
		if strings.HasPrefix(cluster["name"].(string), "delay-") {
			continue
		}
		cluster["type"] = "STATIC"
		assignment := cluster["load_assignment"].(map[string]any)
		for _, group := range assignment["endpoints"].([]any) {
			for _, endpoint := range group.(map[string]any)["lb_endpoints"].([]any) {
				address := endpoint.(map[string]any)["endpoint"].(map[string]any)["address"].(map[string]any)
				address["socket_address"] = map[string]any{"address": "127.0.0.1", "port_value": listener.Addr().(*net.TCPAddr).Port}
			}
		}
	}
	data, err := json.Marshal(bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "envoy.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "envoy.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = log.Close()
	})
	process := exec.Command(binary, "-c", path, "--concurrency", "1", "--disable-hot-restart", "--log-level", "warn")
	process.Stdout, process.Stderr = log, log
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = process.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = process.Process.Kill()
		<-done
		if t.Failed() {
			logs, _ := os.ReadFile(logPath)
			t.Logf("Envoy output:\n%s", logs)
		}
	})
	runtime := fault.NewRuntimeClient("http://127.0.0.1:9901")
	t.Cleanup(runtime.Close)
	ready, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for {
		select {
		case <-done:
			t.Fatalf("Envoy exited during startup: %v", waitErr)
		default:
		}
		if _, err := runtime.Values(ready); err == nil {
			break
		}
		if ready.Err() != nil {
			t.Fatal("Envoy admin did not become ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
	connect := func(address string) *grpc.ClientConn {
		t.Helper()
		conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = conn.Close()
		})
		return conn
	}
	booking, hotels := connect("127.0.0.1:10001"), connect("127.0.0.1:10002")
	invoke := func(conn *grpc.ClientConn, timeout time.Duration) (time.Duration, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), timeout)
		defer cancel()
		ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("traceparent", traceparent, "x-demo-actor", "guest-42"))
		var response wrapperspb.StringValue
		start := time.Now()
		err := conn.Invoke(ctx, "/test.Booking/Create", wrapperspb.String("committed"), &response, grpc.WaitForReady(true))
		if err == nil && response.Value != "committed" {
			t.Fatalf("response body changed: %q", response.Value)
		}
		return time.Since(start), err
	}
	for _, test := range []struct {
		name        string
		direction   string
		unavailable bool
		clear       bool
		timeout     time.Duration
		minElapsed  time.Duration
		code        codes.Code
		calls       int64
	}{
		{name: "baseline", clear: true, timeout: 5 * time.Second, calls: 1},
		{name: "request delay", direction: "request", timeout: 2 * time.Second, minElapsed: 290 * time.Millisecond, calls: 1},
		{name: "response delay", direction: "response", timeout: 2 * time.Second, minElapsed: 290 * time.Millisecond, calls: 1},
		{name: "deadline after execution", direction: "response", timeout: 100 * time.Millisecond, code: codes.DeadlineExceeded, calls: 1},
		{name: "unavailable", direction: "request", unavailable: true, timeout: 2 * time.Second, code: codes.Unavailable},
		{name: "restored", clear: true, timeout: 2 * time.Second, calls: 1},
	} {
		options := fault.Options{From: "api-gateway", To: "booking", Direction: test.direction, Unavailable: test.unavailable}
		if !test.unavailable {
			options.Slowdown = 300 * time.Millisecond
		}
		if test.clear {
			err = fault.ClearRuntime(t.Context(), runtime, options)
		} else {
			err = runtime.Modify(t.Context(), options.Values())
		}
		if err != nil {
			t.Fatalf("%s: configure fault: %v", test.name, err)
		}
		before := calls.Load()
		elapsed, err := invoke(booking, test.timeout)
		if status.Code(err) != test.code || elapsed < test.minElapsed || calls.Load()-before != test.calls {
			t.Fatalf("%s: code=%v want=%v, elapsed=%v want>=%v, upstream calls=%d want=%d", test.name, err, test.code, elapsed, test.minElapsed, calls.Load()-before, test.calls)
		}
		if test.unavailable {
			if _, err := invoke(hotels, 2*time.Second); err != nil {
				t.Fatalf("fault on booking affected independent hotels edge: %v", err)
			}
		}
	}
}
