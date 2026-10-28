//go:build integration

package rtconfig

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
)

// These tests own a separate standalone etcd process, bound only to loopback.
// They never connect to a caller-supplied cluster. Set ETCD_BIN to the etcd binary.
// Example: ETCD_BIN=/path/to/etcd go test -race -tags integration ./internal/pkg/rtconfig
func TestEtcdIntegration(t *testing.T) {
	server := newIntegrationEtcd(t)
	writer := newIntegrationClient(t, server.endpoint)

	t.Run("update_delete_invalid_fix", func(t *testing.T) {
		prefix := integrationPrefix(t)
		m := newIntegrationManager(t, server.endpoint, prefix)
		integrationPut(t, writer, prefix+"a", "true")
		integrationWaitFlag(t, m, "a", true)
		integrationPut(t, writer, prefix+"a", "false")
		integrationWaitFlag(t, m, "a", false)
		integrationPut(t, writer, prefix+"a", "true")
		integrationWaitFlag(t, m, "a", true)
		invalidRevision := integrationPut(t, writer, prefix+"a", `"not-a-bool"`)
		integrationEventually(t, func() bool {
			status := m.Status()
			return status.Invalid && status.ObservedRevision >= invalidRevision
		}, "invalid update must be observed and diagnosed")
		if !integrationFlag(t, m, "a") {
			t.Fatal("invalid update replaced the last valid true value")
		}
		integrationPut(t, writer, prefix+"a", "false")
		integrationWaitFlag(t, m, "a", false)
		integrationEventually(t, func() bool { return !m.Status().Invalid }, "valid update clears invalid status")
		integrationPut(t, writer, prefix+"a", "true")
		integrationWaitFlag(t, m, "a", true)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := writer.Delete(ctx, prefix+"a"); err != nil {
			t.Fatal(err)
		}
		integrationWaitFlag(t, m, "a", false)
	})

	t.Run("invalid_boot_snapshot_retains_raw_candidate", func(t *testing.T) {
		prefix := integrationPrefix(t)
		integrationPut(t, writer, prefix+"a", "true")
		revision := integrationPut(t, writer, prefix+"b", `"invalid"`)
		m := newIntegrationManager(t, server.endpoint, prefix)
		integrationEventually(t, func() bool {
			status := m.Status()
			return status.Invalid && status.ObservedRevision >= revision
		}, "invalid initial snapshot is reported")
		if integrationFlag(t, m, "a") || integrationFlag(t, m, "b") {
			t.Fatal("invalid full snapshot must not be partially published")
		}
		integrationPut(t, writer, prefix+"b", "true")
		integrationWaitFlag(t, m, "a", true)
		integrationWaitFlag(t, m, "b", true)
		integrationEventually(t, func() bool { return !m.Status().Invalid }, "corrected raw candidate becomes valid")
	})

	t.Run("transaction_values_are_atomic", func(t *testing.T) {
		prefix := integrationPrefix(t)
		m := newIntegrationManager(t, server.endpoint, prefix)
		stop := make(chan struct{})
		failures := make(chan error, 1)
		var readers sync.WaitGroup
		for range 4 {
			readers.Go(func() {
				for {
					select {
					case <-stop:
						return
					default:
					}
					values, err := m.Values(context.Background())
					if err != nil || values["a"].Bool() != values["b"].Bool() {
						select {
						case failures <- fmt.Errorf("mixed snapshot or Values error: %v", err):
						default:
						}
						return
					}
				}
			})
		}
		t.Cleanup(func() { close(stop); readers.Wait() })
		var lastRevision int64
		for i := range 60 {
			value := "false"
			if i%2 == 0 {
				value = "true"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			response, err := writer.Txn(ctx).Then(clientv3.OpPut(prefix+"a", value), clientv3.OpPut(prefix+"b", value)).Commit()
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			lastRevision = response.Header.Revision
		}
		integrationEventually(t, func() bool { return m.Status().ObservedRevision >= lastRevision }, "all transaction revisions are processed")
		select {
		case err := <-failures:
			t.Fatal(err)
		default:
		}
	})

	t.Run("restart_same_data_keeps_last_good_and_recovers", func(t *testing.T) {
		prefix := integrationPrefix(t)
		integrationPut(t, writer, prefix+"a", "true")
		m := newIntegrationManager(t, server.endpoint, prefix)
		integrationWaitFlag(t, m, "a", true)
		server.stop(t)
		integrationEventually(t, func() bool { return !m.Status().Connected }, "outage is diagnosed")
		if !integrationFlag(t, m, "a") {
			t.Fatal("outage changed last good value")
		}
		server.start(t)
		integrationPut(t, writer, prefix+"a", "false")
		integrationWaitFlag(t, m, "a", false)
	})

	t.Run("compacted_history_requires_full_resync", func(t *testing.T) {
		prefix := integrationPrefix(t)
		integrationPut(t, writer, prefix+"a", "true")
		integrationPut(t, writer, prefix+"b", "true")
		proxy := newIntegrationProxy(t, strings.TrimPrefix(server.endpoint, "http://"))
		m := newIntegrationManager(t, "http://"+proxy.listener.Addr().String(), prefix)
		integrationWaitFlag(t, m, "a", true)
		integrationWaitFlag(t, m, "b", true)
		observed := m.Status().ObservedRevision
		proxy.setEnabled(false)
		integrationEventually(t, func() bool { return !m.Status().Connected }, "proxy outage is diagnosed")
		integrationPut(t, writer, prefix+"a", "false")
		integrationPut(t, writer, prefix+"a", "true")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		deleted, err := writer.Delete(ctx, prefix+"b")
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if deleted.Header.Revision <= observed+1 {
			t.Fatal("test did not create history beyond the old cursor")
		}
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
		_, err = writer.Compact(ctx, deleted.Header.Revision, clientv3.WithCompactPhysical())
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		proxy.setEnabled(true)
		integrationWaitFlag(t, m, "b", false)
		integrationWaitFlag(t, m, "a", true)
		integrationEventually(t, func() bool { return m.Status().ObservedRevision >= deleted.Header.Revision }, "new snapshot covers compacted revision")
	})

	t.Run("offline_boot_defaults_then_recovery_and_close", func(t *testing.T) {
		prefix := integrationPrefix(t)
		integrationPut(t, writer, prefix+"a", "true")
		proxy := newIntegrationProxy(t, strings.TrimPrefix(server.endpoint, "http://"))
		proxy.setEnabled(false)
		started := time.Now()
		m := newIntegrationManager(t, "http://"+proxy.listener.Addr().String(), prefix)
		if elapsed := time.Since(started); elapsed > 3*time.Second {
			t.Fatalf("offline startup was not bounded: %s", elapsed)
		}
		if integrationFlag(t, m, "a") {
			t.Fatal("offline cold start should use false default")
		}
		proxy.setEnabled(true)
		integrationWaitFlag(t, m, "a", true)
		proxy.setEnabled(false)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Fatalf("Close during outage: %v", err)
		}
		if !m.Status().Closed {
			t.Fatal("closed state not reported")
		}
		if err := m.Close(ctx); err != nil {
			t.Fatalf("Close is not idempotent: %v", err)
		}
	})
}

func TestEtcdAuthenticatedOfflineBootIntegration(t *testing.T) {
	server := newIntegrationEtcd(t)
	admin := newIntegrationClient(t, server.endpoint)
	prefix := integrationPrefix(t)
	integrationPut(t, admin, prefix+"a", "true")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Credentials are disposable fixture values for this owned loopback server.
	const password = "rtconfig-integration-fixture-only"
	if _, err := admin.RoleAdd(ctx, "root"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.UserAdd(ctx, "root", password); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.UserGrantRole(ctx, "root", "root"); err != nil {
		t.Fatal(err)
	}
	for _, role := range []struct {
		name string
		perm clientv3.PermissionType
	}{
		{
			name: "config-reader",
			perm: clientv3.PermissionType(clientv3.PermRead),
		},
		{
			name: "config-writer",
			perm: clientv3.PermissionType(clientv3.PermWrite),
		},
	} {
		if _, err := admin.RoleAdd(ctx, role.name); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.RoleGrantPermission(ctx, role.name, prefix, clientv3.GetPrefixRangeEnd(prefix), role.perm); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.UserAdd(ctx, role.name, password); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.UserGrantRole(ctx, role.name, role.name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := admin.AuthEnable(ctx); err != nil {
		t.Fatal(err)
	}
	reader := newAuthenticatedIntegrationClient(t, server.endpoint, "config-reader", password)
	writer := newAuthenticatedIntegrationClient(t, server.endpoint, "config-writer", password)
	if _, err := reader.Put(ctx, prefix+"a", "false"); !errors.Is(err, rpctypes.ErrPermissionDenied) {
		t.Fatalf("reader must not write, got %v", err)
	}
	if _, err := writer.Put(ctx, "/outside-allowed-prefix", "false"); !errors.Is(err, rpctypes.ErrPermissionDenied) {
		t.Fatalf("writer must not leave its prefix, got %v", err)
	}
	proxy := newIntegrationProxy(t, strings.TrimPrefix(server.endpoint, "http://"))
	proxy.setEnabled(false)
	started := time.Now()
	m := newIntegrationManager(t, "http://"+proxy.listener.Addr().String(), prefix, func(options *Options) {
		options.Username, options.Password = "config-reader", password
	})
	if time.Since(started) > 3*time.Second {
		t.Fatal("authenticated offline startup was not bounded")
	}
	if m.Status().Closed || integrationFlag(t, m, "a") {
		t.Fatal("authentication outage must retain live manager and local false default")
	}
	proxy.setEnabled(true)
	integrationWaitFlag(t, m, "a", true)
	integrationPut(t, writer, prefix+"a", "false")
	integrationWaitFlag(t, m, "a", false)
}

func newAuthenticatedIntegrationClient(t *testing.T, endpoint, username, password string) *clientv3.Client {
	t.Helper()
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{endpoint},
		DialTimeout: 2 * time.Second,
		Username:    username,
		Password:    password,
		Logger:      zap.NewNop(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

const integrationSchemaYAML = `realtimeConfig:
  a:
    type: bool
    value: false
    writable: true
  b:
    type: bool
    value: false
    writable: true
`

func newIntegrationManager(t *testing.T, endpoint, prefix string, customize ...func(*Options)) *Manager {
	t.Helper()
	schema, err := ParseSchema([]byte(integrationSchemaYAML))
	if err != nil {
		t.Fatal(err)
	}
	options := Options{
		Endpoints:      []string{endpoint},
		Prefix:         prefix,
		ProjectID:      "integration",
		RequestTimeout: 250 * time.Millisecond,
		WatchTimeout:   2 * time.Second,
		ProbeInterval:  100 * time.Millisecond,
		ProbeTimeout:   150 * time.Millisecond,
		StaleAfter:     600 * time.Millisecond,
		RetryMin:       20 * time.Millisecond,
		RetryMax:       100 * time.Millisecond,
	}
	for _, apply := range customize {
		apply(&options)
	}
	m, err := New(schema, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Errorf("manager cleanup: %v", err)
		}
	})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return m
}

func integrationPrefix(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("/rtconfig-integration/%d/%s/", time.Now().UnixNano(), strings.ReplaceAll(t.Name(), "/", "-"))
}

func integrationEventually(t *testing.T, predicate func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", what)
}

func integrationFlag(t *testing.T, m *Manager, key string) bool {
	t.Helper()
	value, err := m.Value(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	return value.Bool()
}

func integrationWaitFlag(t *testing.T, m *Manager, key string, want bool) {
	t.Helper()
	integrationEventually(t, func() bool { return integrationFlag(t, m, key) == want }, fmt.Sprintf("%s=%t; status=%+v", key, want, m.Status()))
}

func integrationPut(t *testing.T, client *clientv3.Client, key, value string) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	response, err := client.Put(ctx, key, value)
	if err != nil {
		t.Fatal(err)
	}
	return response.Header.Revision
}

func newIntegrationClient(t *testing.T, endpoint string) *clientv3.Client {
	t.Helper()
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{endpoint},
		DialTimeout: time.Second,
		Logger:      zap.NewNop(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

type integrationEtcd struct {
	binary   string
	endpoint string
	peerURL  string
	dataDir  string
	logPath  string
	command  *exec.Cmd
	exited   chan error
	log      *os.File
}

func newIntegrationEtcd(t *testing.T) *integrationEtcd {
	t.Helper()
	binary := os.Getenv("ETCD_BIN")
	if binary == "" {
		var err error
		binary, err = exec.LookPath("etcd")
		if err != nil {
			t.Skip("integration tests require ETCD_BIN pointing to a standalone etcd binary")
		}
	}
	dir := t.TempDir()
	s := &integrationEtcd{
		binary:   binary,
		endpoint: "http://" + integrationFreeAddress(t),
		peerURL:  "http://" + integrationFreeAddress(t),
		dataDir:  filepath.Join(dir, "data"),
		logPath:  filepath.Join(dir, "etcd.log"),
	}
	t.Cleanup(func() { s.stop(t) })
	s.start(t)
	return s
}

func integrationFreeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return address
}

func (s *integrationEtcd) start(t *testing.T) {
	t.Helper()
	var err error
	s.log, err = os.OpenFile(s.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	s.command = exec.Command(s.binary,
		"--name", "rtconfig-integration", "--data-dir", s.dataDir,
		"--listen-client-urls", s.endpoint, "--advertise-client-urls", s.endpoint,
		"--listen-peer-urls", s.peerURL, "--initial-advertise-peer-urls", s.peerURL,
		"--initial-cluster", "rtconfig-integration="+s.peerURL,
		"--initial-cluster-token", "rtconfig-isolated-test", "--log-level", "error")
	s.command.Stdout, s.command.Stderr = s.log, s.log
	if err := s.command.Start(); err != nil {
		_ = s.log.Close()
		t.Fatal(err)
	}
	s.exited = make(chan error, 1)
	go func(command *exec.Cmd, exited chan<- error) { exited <- command.Wait() }(s.command, s.exited)
	client := newIntegrationClient(t, s.endpoint)
	integrationEventually(t, func() bool {
		select {
		case err := <-s.exited:
			log, _ := os.ReadFile(s.logPath)
			s.command = nil
			t.Fatalf("etcd exited during startup: %v\n%s", err, log)
		default:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_, err := client.Get(ctx, "/rtconfig-readiness-probe")
		return err == nil
	}, "standalone etcd becomes ready")
	_ = client.Close()
}

func (s *integrationEtcd) stop(t *testing.T) {
	t.Helper()
	if s.command == nil {
		return
	}
	_ = s.command.Process.Signal(os.Interrupt)
	select {
	case <-s.exited:
	case <-time.After(5 * time.Second):
		_ = s.command.Process.Kill()
		<-s.exited
		t.Error("owned etcd process required forced termination")
	}
	_ = s.log.Close()
	s.command = nil
}

// The proxy makes only the manager's transport unavailable while an independent
// administrative client can update and compact the owned etcd server.
type integrationProxy struct {
	listener net.Listener
	target   string
	mu       sync.Mutex
	enabled  bool
	active   map[net.Conn]struct{}
	wg       sync.WaitGroup
}

func newIntegrationProxy(t *testing.T, target string) *integrationProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &integrationProxy{
		listener: listener,
		target:   target,
		enabled:  true,
		active:   make(map[net.Conn]struct{}),
	}
	p.wg.Go(func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			p.wg.Go(func() { p.forward(connection) })
		}
	})
	t.Cleanup(func() { _ = listener.Close(); p.setEnabled(false); p.wg.Wait() })
	return p
}

func (p *integrationProxy) setEnabled(enabled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.enabled = enabled
	if !enabled {
		for connection := range p.active {
			_ = connection.Close()
		}
	}
}

func (p *integrationProxy) forward(client net.Conn) {
	defer client.Close()
	upstream, err := net.DialTimeout("tcp", p.target, 200*time.Millisecond)
	if err != nil {
		return
	}
	defer upstream.Close()
	p.mu.Lock()
	if !p.enabled {
		p.mu.Unlock()
		return
	}
	p.active[client], p.active[upstream] = struct{}{}, struct{}{}
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.active, client); delete(p.active, upstream); p.mu.Unlock() }()
	done := make(chan struct{})
	go func() { _, _ = io.Copy(upstream, client); _ = upstream.Close(); close(done) }()
	_, _ = io.Copy(client, upstream)
	_ = client.Close()
	<-done
}

// Only freshly started readers wait for recovery; an already synchronized
// reader keeps serving from its last valid snapshot during an etcd outage.
func TestInitialSyncReadinessEtcdIntegration(t *testing.T) {
	server := newIntegrationEtcd(t)
	prefix := integrationPrefix(t)
	writer := newIntegrationClient(t, server.endpoint)
	integrationPut(t, writer, prefix+"a", "true")
	old := newIntegrationManager(t, server.endpoint, prefix)
	if err := old.CheckInitialSync(); err != nil {
		t.Fatal(err)
	}
	server.stop(t)
	fresh := newIntegrationManager(t, server.endpoint, prefix, func(opts *Options) {
		opts.StartupTimeout = 100 * time.Millisecond
	})
	if !errors.Is(fresh.CheckInitialSync(), ErrNotSynced) {
		t.Fatal("new reader admitted before initial sync")
	}
	if err := old.CheckInitialSync(); err != nil || !integrationFlag(t, old, "a") {
		t.Fatalf("old reader lost readiness or snapshot: %v", err)
	}
	server.start(t)
	integrationEventually(t, func() bool {
		return fresh.CheckInitialSync() == nil && integrationFlag(t, fresh, "a")
	}, "new reader loads snapshot and becomes ready")
	integrationPut(t, writer, prefix+"a", "false")
	integrationWaitFlag(t, fresh, "a", false)
	if err := fresh.CheckInitialSync(); err != nil {
		t.Fatalf("disabling a flag must not change readiness: %v", err)
	}
}
