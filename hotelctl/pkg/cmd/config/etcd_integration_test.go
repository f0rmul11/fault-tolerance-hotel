//go:build integration

package config

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func runConfig(t *testing.T, f localFactory, endpoint string, args ...string) (string, error) {
	t.Helper()
	var output, diagnostic bytes.Buffer
	command := NewCmdConfig(f, iostreams.IOStreams{
		Out:    &output,
		ErrOut: &diagnostic,
	})
	command.SetArgs(append(args, "--endpoints", endpoint))
	err := command.ExecuteContext(context.Background())
	return output.String(), err
}

func TestConfigEtcdIntegration(t *testing.T) {
	server := startEtcd(t)
	f := fixture(t)
	catalogPath := filepath.Join(f.config.Root, "services/api-gateway/config/rtconfig.yaml")
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte(`  quota: {type: uint64, value: 0, writable: true}
  delay: {type: duration, value: 1s, writable: true}
  message: {type: string, value: "", writable: true}
`)...)
	if err := os.WriteFile(catalogPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runConfig(t, f, server.endpoint, "get", "api-gateway", "flag")
	if err != nil || !strings.Contains(out, "false (source=default revision=0") {
		t.Fatalf("missing override: %s %v", out, err)
	}

	const prefix = "/hotelflow/dev/services/api-gateway/rtconfig/v1/values/"
	for _, tt := range []struct {
		name  string
		input string
		wire  string
	}{
		{
			name:  "flag",
			input: "true",
			wire:  "true",
		},
		{
			name:  "quota",
			input: "18446744073709551615",
			wire:  "18446744073709551615",
		},
		{
			name:  "delay",
			input: "1500ms",
			wire:  `"1.5s"`,
		},
		{
			name:  "message",
			input: "maintenance\nsoon",
			wire:  `"maintenance\nsoon"`,
		},
		{
			name:  "mode",
			input: "degraded",
			wire:  `"degraded"`,
		},
	} {
		out, err := runConfig(t, f, server.endpoint, "set", "api-gateway", tt.name, tt.input, "--expect-revision=0")
		if err != nil || !strings.Contains(out, "revision=") {
			t.Fatalf("set %s: %s %v", tt.name, out, err)
		}
		response, err := server.client.Get(t.Context(), prefix+tt.name)
		if err != nil || len(response.Kvs) != 1 || string(response.Kvs[0].Value) != tt.wire {
			t.Fatalf("stored %s: %v %v", tt.name, response, err)
		}
	}
	current, err := server.client.Get(t.Context(), prefix+"flag")
	if err != nil {
		t.Fatal(err)
	}
	revision := current.Kvs[0].ModRevision
	out, err = runConfig(t, f, server.endpoint, "set", "api-gateway", "flag", "false", "--expect-revision=0")
	if err == nil || !strings.Contains(err.Error(), "expected revision 0") || out != "" {
		t.Fatalf("stale CAS must fail without success output: %s %v", out, err)
	}
	current, err = server.client.Get(t.Context(), prefix+"flag")
	if err != nil || current.Kvs[0].ModRevision != revision || string(current.Kvs[0].Value) != "true" {
		t.Fatalf("conflict changed value: %v %v", current, err)
	}
	out, err = runConfig(t, f, server.endpoint, "set", "api-gateway", "flag", "false", "--expect-revision="+strconv.FormatInt(revision, 10))
	if err != nil || !strings.Contains(out, "= false") {
		t.Fatalf("valid CAS: %s %v", out, err)
	}

	// The same name in another service/environment is a separate key.
	reviewPath := filepath.Join(f.config.Root, "services/review/config")
	if err := os.MkdirAll(reviewPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reviewPath, "rtconfig.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runConfig(t, f, server.endpoint, "set", "review", "flag", "true"); err != nil {
		t.Fatal(err)
	}
	if _, err := runConfig(t, f, server.endpoint, "set", "api-gateway", "flag", "true", "--env=other"); err != nil {
		t.Fatal(err)
	}
	out, err = runConfig(t, f, server.endpoint, "get", "api-gateway", "flag")
	if err != nil || !strings.Contains(out, "= false (source=etcd") {
		t.Fatalf("service/env isolation lost: %s %v", out, err)
	}
	if _, err := runConfig(t, f, server.endpoint, "set", "api-gateway", "readonly", "false"); err == nil {
		t.Fatal("readonly update accepted")
	}
	response, err := server.client.Get(t.Context(), prefix+"readonly")
	if err != nil || len(response.Kvs) != 0 {
		t.Fatalf("readonly variable was written: %v %v", response, err)
	}

	server.stop()
	started := time.Now()
	out, err = runConfig(t, f, server.endpoint, "set", "api-gateway", "flag", "true", "--timeout=100ms")
	if err == nil || out != "" || time.Since(started) > 2*time.Second {
		t.Fatalf("offline command must fail within budget: %s %v", out, err)
	}
}

func TestConfigEtcdAuthenticationIntegration(t *testing.T) {
	server := startEtcd(t)
	f := fixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	const password = "isolated-etcd-test-password"
	const prefix = "/hotelflow/dev/services/api-gateway/rtconfig/v1/values/"
	if _, err := server.client.UserAdd(ctx, "root", password); err != nil {
		t.Fatal(err)
	}
	if _, err := server.client.UserGrantRole(ctx, "root", "root"); err != nil {
		t.Fatal(err)
	}
	if _, err := server.client.RoleAdd(ctx, "config-writer"); err != nil {
		t.Fatal(err)
	}
	if _, err := server.client.RoleGrantPermission(ctx, "config-writer", prefix, clientv3.GetPrefixRangeEnd(prefix), clientv3.PermissionType(clientv3.PermReadWrite)); err != nil {
		t.Fatal(err)
	}
	if _, err := server.client.UserAdd(ctx, "operator", password); err != nil {
		t.Fatal(err)
	}
	if _, err := server.client.UserGrantRole(ctx, "operator", "config-writer"); err != nil {
		t.Fatal(err)
	}
	if _, err := server.client.AuthEnable(ctx); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOTELCTL_INTEGRATION_PASSWORD", password)
	args := []string{"set", "api-gateway", "flag", "true", "--user=operator", "--password-env=HOTELCTL_INTEGRATION_PASSWORD"}
	out, err := runConfig(t, f, server.endpoint, args...)
	if err != nil || !strings.Contains(out, "= true") {
		t.Fatalf("authenticated writer: %s %v", out, err)
	}
	out, err = runConfig(t, f, server.endpoint, append(args, "--env=forbidden")...)
	if err == nil || out != "" || strings.Contains(err.Error(), password) {
		t.Fatalf("RBAC must deny another prefix without printing credentials: %s %v", out, err)
	}
}

type etcdProcess struct {
	endpoint string
	client   *clientv3.Client
	stop     func()
}

// Every integration test owns its etcd process, random loopback ports and temp data.
func startEtcd(t *testing.T) *etcdProcess {
	t.Helper()
	binary := os.Getenv("ETCD_BIN")
	if binary == "" {
		t.Skip("set ETCD_BIN to a standalone etcd binary")
	}
	listeners := make([]net.Listener, 0, 2)
	for range 2 {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listeners = append(listeners, listener)
		t.Cleanup(func() {
			listener.Close()
		})
	}
	endpoint := "http://" + listeners[0].Addr().String()
	peer := "http://" + listeners[1].Addr().String()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "etcd.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		log.Close()
	})
	command := exec.Command(binary,
		"--name", "hotelctl-integration",
		"--data-dir", filepath.Join(dir, "data"),
		"--listen-client-urls", endpoint,
		"--advertise-client-urls", endpoint,
		"--listen-peer-urls", peer,
		"--initial-advertise-peer-urls", peer,
		"--initial-cluster", "hotelctl-integration="+peer,
		"--initial-cluster-token", "hotelctl-isolated-test",
		"--log-level", "error",
	)
	command.Stdout, command.Stderr = log, log
	for _, listener := range listeners {
		listener.Close()
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
	}()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		command.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			command.Process.Kill()
			<-done
			t.Error("owned etcd needed forced termination")
		}
	}
	t.Cleanup(stop)
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{endpoint},
		DialTimeout: time.Second,
		Logger:      zap.NewNop(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		client.Close()
	})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if _, err := client.Get(ctx, "/readiness"); err != nil {
		data, _ := os.ReadFile(logPath)
		t.Fatal(fmt.Errorf("etcd startup: %w\n%s", err, data))
	}
	return &etcdProcess{
		endpoint: endpoint,
		client:   client,
		stop:     stop,
	}
}

// This controlled server checks the CLI's uncertain-response policy separately
// from the real-etcd tests above. It is not evidence of server-side durability.
type unconfirmedKV struct {
	etcdserverpb.UnimplementedKVServer
	calls atomic.Int32
}

func (s *unconfirmedKV) Txn(context.Context, *etcdserverpb.TxnRequest) (*etcdserverpb.TxnResponse, error) {
	s.calls.Add(1)
	return nil, status.Error(codes.Unavailable, "write acknowledgement lost")
}

func TestConfigUnconfirmedWriteIsNotRetried(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	backend := &unconfirmedKV{}
	etcdserverpb.RegisterKVServer(server, backend)
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		listener.Close()
		<-done
	})
	out, err := runConfig(t, fixture(t), "http://"+listener.Addr().String(),
		"set", "api-gateway", "flag", "true", "--expect-revision=0")
	if err == nil || !strings.Contains(err.Error(), "did not confirm") || out != "" {
		t.Fatalf("unconfirmed write must report error without success: %s %v", out, err)
	}
	if backend.calls.Load() != 1 {
		t.Fatalf("unconfirmed write was retried: calls=%d", backend.calls.Load())
	}
}
