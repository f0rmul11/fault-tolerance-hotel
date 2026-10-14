package internal

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	reflection "google.golang.org/grpc/reflection/grpc_reflection_v1"

	"hotels/internal/application/usecase"
	"hotels/internal/pkg/closer"
	"hotels/internal/pkg/healthcheck"
	echoV1 "hotels/internal/pkg/pb/hotels/echo/v1"
)

// Проверяем App с Clay, настоящими TCP listeners и сгенерированными
// gRPC обработчиками, а не отдельную тривиальную реализацию Echo.
func TestAppTransportsAndShutdown(t *testing.T) {
	httpPort, grpcPort := availablePort(t), availablePort(t)
	for httpPort == grpcPort {
		grpcPort = availablePort(t)
	}
	adminPort := availablePort(t)
	for adminPort == grpcPort || adminPort == httpPort {
		adminPort = availablePort(t)
	}
	configPath, err := filepath.Abs("../config/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_PATH", configPath)
	t.Setenv("GRPC_PORT", grpcPort)
	t.Setenv("HTTP_PORT", httpPort)
	t.Setenv("ADMIN_HTTP_PORT", adminPort)
	t.Setenv("GRACEFUL_TIMEOUT", "2s")
	process := exec.Command(os.Args[0], "-test.run=^TestServiceProcess$")
	process.Env = append(os.Environ(), "HOTEL_SERVICE_HELPER=1")
	var logs bytes.Buffer
	process.Stdout = &logs
	process.Stderr = &logs
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- process.Wait()
	}()
	t.Cleanup(func() {
		_ = process.Process.Signal(syscall.SIGTERM)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("shutdown: %v\n%s", err, logs.String())
			}
		case <-time.After(8 * time.Second):
			_ = process.Process.Kill()
			<-done
			t.Errorf("shutdown deadline exceeded\n%s", logs.String())
		}
	})

	client := &http.Client{
		Timeout: time.Second,
	}
	defer client.CloseIdleConnections()
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := client.Get("http://127.0.0.1:" + adminPort + "/healthcheck/ready")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("app did not become ready")
		}
		time.Sleep(20 * time.Millisecond)
	}

	conn, err := grpc.NewClient("127.0.0.1:"+grpcPort, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	grpcClient := echoV1.NewEchoServiceClient(conn)
	for _, message := range []string{
		"HotelFlow",
		"Привет, отель! & + / ?",
		"",
		"line one\nline two",
	} {
		t.Run(fmt.Sprintf("message=%q", message), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			result, err := grpcClient.Echo(ctx, &echoV1.EchoRequest{
				Message: message,
			}, grpc.WaitForReady(true))
			if err != nil || result.GetMessage() != message || result.GetService() != "hotels" {
				t.Fatalf("gRPC Echo = %v, %v", result, err)
			}
		})
	}
	t.Run("no HTTP RPC", func(t *testing.T) {
		for _, path := range []string{
			"/v1/echo",
			"/hotels.echo.v1.EchoService/Echo",
		} {
			response, err := client.Post(
				"http://127.0.0.1:"+httpPort+path,
				"application/json",
				bytes.NewBufferString(`{"message":"test"}`),
			)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusNotFound {
				t.Fatalf("HTTP RPC should not be registered: %s returned %d", path, response.StatusCode)
			}
		}
	})
	t.Run("reflection", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		stream, err := reflection.NewServerReflectionClient(conn).ServerReflectionInfo(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer stream.CloseSend()
		if err := stream.Send(&reflection.ServerReflectionRequest{
			MessageRequest: &reflection.ServerReflectionRequest_ListServices{
				ListServices: "",
			},
		}); err != nil {
			t.Fatal(err)
		}
		result, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		for _, service := range result.GetListServicesResponse().GetService() {
			if service.Name == "hotels.echo.v1.EchoService" {
				return
			}
		}
		t.Fatal("Echo service not advertised through reflection")
	})
}

func availablePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return fmt.Sprint(listener.Addr().(*net.TCPAddr).Port)
}

func TestServiceProcess(t *testing.T) {
	if os.Getenv("HOTEL_SERVICE_HELPER") != "1" {
		return
	}
	ctx := context.Background()
	app := &App{
		grpcConn:     make(map[string]grpc.ClientConnInterface),
		publicCloser: closer.New(syscall.SIGTERM, syscall.SIGINT),
		adminCloser:  closer.New(),
		usecases:     &usecase.Registry{},
	}
	if err := app.initAdminServer(ctx); err != nil {
		t.Fatal(err)
	}
	app.runAdminServer(ctx)
	if err := app.initMainServer(ctx); err != nil {
		t.Fatal(err)
	}
	if err := app.initControllers(ctx); err != nil {
		t.Fatal(err)
	}
	app.Run(ctx)
}

func TestHealthLifecycle(t *testing.T) {
	app := &App{
		publicCloser: closer.New(),
	}
	if err := app.initHealthCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	check := func(path string, want int) {
		t.Helper()
		response := httptest.NewRecorder()
		app.healthCheck.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != want {
			t.Fatalf("%s: got %d, want %d: %s", path, response.Code, want, response.Body.String())
		}
	}
	check(healthcheck.StartupPath, http.StatusServiceUnavailable)
	check(healthcheck.ReadinessPath, http.StatusServiceUnavailable)
	check(healthcheck.LivenessPath, http.StatusOK)
	atomic.StoreInt32(&app.started, 1)
	check(healthcheck.StartupPath, http.StatusOK)
	check(healthcheck.ReadinessPath, http.StatusOK)
	atomic.StoreInt32(&app.terminated, 1)
	check(healthcheck.ReadinessPath, http.StatusServiceUnavailable)
	check(healthcheck.LivenessPath, http.StatusOK)
}

// Admin is already serving while neither initMainServer nor Run has executed.
func TestAdminStartsBeforeMain(t *testing.T) {
	configPath, err := filepath.Abs("../config/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	adminPort := availablePort(t)
	t.Setenv("CONFIG_PATH", configPath)
	t.Setenv("ADMIN_HTTP_PORT", adminPort)
	app := &App{
		publicCloser: closer.New(),
		adminCloser:  closer.New(),
	}
	ctx := context.Background()
	if err := app.initAdminServer(ctx); err != nil {
		t.Fatal(err)
	}
	app.runAdminServer(ctx)
	t.Cleanup(app.adminCloser.CloseAll)
	if app.mainServer != nil {
		t.Fatal("main server must not be initialized yet")
	}
	client := &http.Client{
		Timeout: time.Second,
	}
	defer client.CloseIdleConnections()
	for path, want := range map[string]int{
		healthcheck.LivenessPath:  http.StatusOK,
		healthcheck.ReadinessPath: http.StatusServiceUnavailable,
		healthcheck.StartupPath:   http.StatusServiceUnavailable,
	} {
		response, err := client.Get("http://127.0.0.1:" + adminPort + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("%s: got %d, want %d", path, response.StatusCode, want)
		}
	}
}
