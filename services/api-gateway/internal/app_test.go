package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"api-gateway/internal/pkg/closer"
	"api-gateway/internal/pkg/healthcheck"

	echoV1 "api-gateway/internal/pkg/pb/api-gateway/echo/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Проверяем именно App с Clay, настоящими TCP listeners и сгенерированными
// HTTP/gRPC обработчиками, а не отдельную тривиальную реализацию Echo.
func TestAppTransportsAndShutdown(t *testing.T) {
	httpPort, grpcPort := availablePort(t), availablePort(t)
	for grpcPort == httpPort {
		grpcPort = availablePort(t)
	}
	adminPort := availablePort(t)
	for adminPort == httpPort || adminPort == grpcPort {
		adminPort = availablePort(t)
	}
	configPath, err := filepath.Abs("../config/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_PATH", configPath)
	t.Setenv("HTTP_PORT", httpPort)
	t.Setenv("GRPC_PORT", grpcPort)
	t.Setenv("ADMIN_HTTP_PORT", adminPort)
	t.Setenv("GRACEFUL_TIMEOUT", "2s")
	process := exec.Command(os.Args[0], "-test.run=^TestGatewayProcess$")
	process.Env = append(os.Environ(), "HOTEL_GATEWAY_HELPER=1")
	var logs bytes.Buffer
	process.Stdout = &logs
	process.Stderr = &logs
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
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

	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	base := "http://127.0.0.1:" + httpPort
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := client.Get("http://127.0.0.1:" + adminPort + "/healthcheck/ready")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				apiResponse, apiErr := client.Get(base + "/v1/echo")
				if apiErr == nil {
					apiResponse.Body.Close()
					if apiResponse.StatusCode == http.StatusOK {
						break
					}
				}
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
	for _, message := range []string{"HotelFlow", "Привет, отель! & + / ?", "", "line one\nline two"} {
		t.Run(fmt.Sprintf("message=%q", message), func(t *testing.T) {
			payload, _ := json.Marshal(map[string]string{"message": message})
			response, err := client.Post(base+"/v1/echo", "application/json", bytes.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			assertEcho(t, response, message)
			response, err = client.Get(base + "/v1/echo?message=" + url.QueryEscape(message))
			if err != nil {
				t.Fatal(err)
			}
			assertEcho(t, response, message)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			result, err := grpcClient.Echo(ctx, &echoV1.EchoRequest{Message: message}, grpc.WaitForReady(true))
			if err != nil || result.GetMessage() != message {
				t.Fatalf("gRPC Echo = %v, %v", result, err)
			}
		})
	}
	t.Run("invalid JSON", func(t *testing.T) {
		response, err := client.Post(base+"/v1/echo", "application/json", strings.NewReader(`{"message":`))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid JSON status: %d", response.StatusCode)
		}
	})
	t.Run("OpenAPI and docs", func(t *testing.T) {
		response, err := client.Get(base + "/swagger.json")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var spec struct {
			Paths map[string]map[string]json.RawMessage `json:"paths"`
		}
		if err := json.NewDecoder(response.Body).Decode(&spec); err != nil {
			t.Fatal(err)
		}
		if spec.Paths["/v1/echo"]["get"] == nil || spec.Paths["/v1/echo"]["post"] == nil {
			t.Fatal("OpenAPI must describe both Echo bindings")
		}
		response, err = client.Get(base + "/docs/")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != 200 || !bytes.Contains(body, []byte("swagger")) {
			t.Fatalf("Swagger UI: %d", response.StatusCode)
		}
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

func assertEcho(t *testing.T, response *http.Response, want string) {
	t.Helper()
	defer response.Body.Close()
	var result struct {
		Message string `json:"message"`
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("HTTP status %d: %s", response.StatusCode, body)
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Message != want {
		t.Fatalf("Echo message = %q, want %q", result.Message, want)
	}
}

func TestGatewayProcess(t *testing.T) {
	if os.Getenv("HOTEL_GATEWAY_HELPER") != "1" {
		return
	}
	ctx := context.Background()
	New(ctx).Run(ctx)
}

func TestHealthLifecycle(t *testing.T) {
	app := &App{publicCloser: closer.New()}
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
	app := &App{publicCloser: closer.New(), adminCloser: closer.New()}
	ctx := context.Background()
	if err := app.initAdminServer(ctx); err != nil {
		t.Fatal(err)
	}
	app.runAdminServer(ctx)
	t.Cleanup(app.adminCloser.CloseAll)
	if app.mainServer != nil {
		t.Fatal("main server must not be initialized yet")
	}
	client := &http.Client{Timeout: time.Second}
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
