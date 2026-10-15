package rtconfig

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	schema, err := NewSchema(Definition{
		Name:     "flag",
		Type:     BoolType,
		Default:  false,
		Writable: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	manager, err := New(schema, Options{
		Endpoints:     []string{"http://127.0.0.1:2379"},
		Prefix:        "/test/",
		ProbeInterval: time.Hour,
		RetryMin:      time.Millisecond,
		RetryMax:      2 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	return manager
}

type fakeBackend struct {
	get      func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error)
	watch    func(context.Context, string, ...clientv3.OpOption) clientv3.WatchChan
	progress func(context.Context) error
	close    func() error
}

func (b *fakeBackend) Get(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	return b.get(ctx, key, opts...)
}

func (b *fakeBackend) Watch(ctx context.Context, key string, opts ...clientv3.OpOption) clientv3.WatchChan {
	return b.watch(ctx, key, opts...)
}

func (b *fakeBackend) RequestProgress(ctx context.Context) error {
	if b.progress != nil {
		return b.progress(ctx)
	}

	return nil
}

func (b *fakeBackend) Close() error {
	if b.close != nil {
		return b.close()
	}

	return nil
}

func getResponse(cluster uint64, revision int64, pairs map[string]string) *clientv3.GetResponse {
	response := &clientv3.GetResponse{
		Header: &etcdserverpb.ResponseHeader{
			ClusterId: cluster,
			Revision:  revision,
		},
		Count: int64(len(pairs)),
	}
	for key, value := range pairs {
		response.Kvs = append(response.Kvs, &mvccpb.KeyValue{
			Key:         []byte(key),
			Value:       []byte(value),
			ModRevision: revision,
		})
	}

	return response
}

func closeManager(t *testing.T, manager *Manager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := manager.Close(ctx); err != nil {
		t.Errorf("close manager: %v", err)
	}
}

func TestManagerRequiresValidatedSchema(t *testing.T) {
	opts := Options{
		Endpoints: []string{"http://127.0.0.1:2379"},
		Prefix:    "/test/",
	}
	for _, schema := range []*Schema{nil, {}} {
		if _, err := New(schema, opts); err == nil {
			t.Fatal("unvalidated schema was accepted")
		}
	}

	// An explicitly validated empty catalog is useful before a service defines
	// its first flag; a zero-value Schema must not bypass schema validation.
	schema, err := NewSchema()
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(schema, opts)
	if err != nil {
		t.Fatal(err)
	}
	closeManager(t, manager)
}

func TestOfflineOpenRecoversWithoutFailingStartup(t *testing.T) {
	manager := newTestManager(t)
	defer closeManager(t, manager)
	var opens atomic.Int32
	var closes atomic.Int32
	watching := make(chan struct{})
	updates := make(chan clientv3.WatchResponse)
	store := &fakeBackend{
		get: func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			return getResponse(1, 2, map[string]string{"/test/flag": "true"}), nil
		},
		watch: func(context.Context, string, ...clientv3.OpOption) clientv3.WatchChan {
			close(watching)
			return updates
		},
		close: func() error {
			closes.Add(1)
			return nil
		},
	}
	manager.open = func(context.Context) (backend, error) {
		if opens.Add(1) == 1 {
			return nil, context.DeadlineExceeded
		}

		return store, nil
	}

	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("offline opening must retain defaults: %v", err)
	}
	select {
	case <-watching:
	case <-time.After(2 * time.Second):
		t.Fatal("manager did not retry opening etcd")
	}

	value, err := manager.Value(context.Background(), "flag")
	if err != nil || !value.Bool() {
		t.Fatalf("recovered value = %v, error = %v", value, err)
	}
	closeManager(t, manager)
	if closes.Load() != 1 {
		t.Fatalf("backend closed %d times", closes.Load())
	}
}

func TestOfflineFetchRetainsDefaultsAndReady(t *testing.T) {
	manager := newTestManager(t)
	defer closeManager(t, manager)
	store := &fakeBackend{
		get: func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			return nil, context.DeadlineExceeded
		},
	}
	manager.open = func(context.Context) (backend, error) {
		return store, nil
	}

	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	value, err := manager.Value(ctx, "flag")
	if err != nil || value.Bool() {
		t.Fatalf("offline defaults unavailable: %v", err)
	}
	if err := <-manager.Ready(ctx); err != nil {
		t.Fatalf("local readiness waits for storage: %v", err)
	}
	if manager.Status().Source != "defaults" || !manager.Status().Stale {
		t.Fatalf("unexpected offline status: %+v", manager.Status())
	}
}

func TestLifecycleCancellationAndClosedReads(t *testing.T) {
	manager := newTestManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("start cancelled context: %v", err)
	}

	closeManager(t, manager)
	closeManager(t, manager)
	if err := manager.Start(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("start closed manager: %v", err)
	}
	if _, err := manager.Value(context.Background(), "flag"); err != nil {
		t.Fatalf("closing invalidated an immutable snapshot: %v", err)
	}
}

func TestCloseBudgetDoesNotHideBlockedCallback(t *testing.T) {
	manager := newTestManager(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	store := &fakeBackend{
		get: func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			return getResponse(1, 1, nil), nil
		},
		watch: func(context.Context, string, ...clientv3.OpOption) clientv3.WatchChan {
			return make(chan clientv3.WatchResponse)
		},
	}
	manager.open = func(context.Context) (backend, error) {
		return store, nil
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer closeManager(t, manager)
	defer close(release)
	if err := manager.WatchVariable(context.Background(), "flag", func(Variable, Variable) {
		close(entered)
		<-release
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("callback did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := manager.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected bounded close, got %v", err)
	}
}

func TestStartupBudgetDoesNotCancelRecovery(t *testing.T) {
	manager := newTestManager(t)
	manager.opts.StartupTimeout = 10 * time.Millisecond
	release := make(chan struct{})
	watching := make(chan struct{})
	store := &fakeBackend{
		get: func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			return getResponse(1, 2, map[string]string{"/test/flag": "true"}), nil
		},
		watch: func(context.Context, string, ...clientv3.OpOption) clientv3.WatchChan {
			close(watching)
			return make(chan clientv3.WatchResponse)
		},
	}
	manager.open = func(ctx context.Context) (backend, error) {
		select {
		case <-release:
			return store, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	defer closeManager(t, manager)

	started := make(chan error, 1)
	go func() {
		started <- manager.Start(context.Background())
	}()
	select {
	case err := <-started:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start exceeded its initial budget")
	}
	if manager.Status().Source != "defaults" {
		t.Fatal("startup should still use defaults")
	}

	close(release)
	select {
	case <-watching:
	case <-time.After(time.Second):
		t.Fatal("startup timeout cancelled background recovery")
	}
	value, _ := manager.Value(context.Background(), "flag")
	if !value.Bool() {
		t.Fatal("recovery did not publish remote value")
	}
}
