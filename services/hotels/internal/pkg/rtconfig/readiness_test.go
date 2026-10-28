package rtconfig

import (
	"context"
	"errors"
	"testing"

	clientv3 "go.etcd.io/etcd/client/v3"
)

func TestInitialSyncReadiness(t *testing.T) {
	var absent *Manager
	if !errors.Is(absent.CheckInitialSync(), ErrNotInitialized) {
		t.Fatal("nil client must not be ready")
	}
	m := newTestManager(t)
	t.Cleanup(func() {
		closeManager(t, m)
	})
	if !errors.Is(m.CheckInitialSync(), ErrNotInitialized) {
		t.Fatal("client must not be ready before Start")
	}
	m.open = func(context.Context) (backend, error) {
		return &fakeBackend{
			get: func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
				return nil, context.DeadlineExceeded
			},
		}, nil
	}
	if err := m.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := m.CheckReady(); err != nil {
		t.Fatalf("local defaults must still be usable: %v", err)
	}
	if !errors.Is(m.CheckInitialSync(), ErrNotSynced) {
		t.Fatal("defaults must not satisfy initial synchronization")
	}
	m.apply(map[string][]byte{"flag": []byte(`"invalid bool"`)}, 1, 1)
	if !errors.Is(m.CheckInitialSync(), ErrNotSynced) {
		t.Fatal("invalid first snapshot must not admit traffic")
	}
	// A valid snapshot with false is ready: readiness checks initialization,
	// not whether an operator has enabled a particular feature.
	m.apply(map[string][]byte{"flag": []byte("false")}, 1, 2)
	if err := m.CheckInitialSync(); err != nil {
		t.Fatalf("valid disabled flag must be ready: %v", err)
	}
	m.connected(false)
	m.failed(context.DeadlineExceeded)
	m.apply(map[string][]byte{"flag": []byte(`"invalid update"`)}, 1, 3)
	if err := m.CheckInitialSync(); err != nil {
		t.Fatalf("last valid snapshot must survive disconnect and invalid update: %v", err)
	}
	value, err := m.Value(t.Context(), "flag")
	if err != nil || value.Bool() {
		t.Fatalf("last valid value lost: %v", err)
	}
	closeManager(t, m)
	if !errors.Is(m.CheckInitialSync(), ErrClosed) {
		t.Fatal("closed client must not be ready")
	}
}

func TestInitialSyncAcceptsValidEmptySnapshot(t *testing.T) {
	m := newTestManager(t)
	t.Cleanup(func() {
		closeManager(t, m)
	})
	m.open = func(context.Context) (backend, error) {
		return &fakeBackend{
			get: func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
				return getResponse(1, 1, nil), nil
			},
			watch: func(context.Context, string, ...clientv3.OpOption) clientv3.WatchChan {
				return make(chan clientv3.WatchResponse)
			},
		}, nil
	}
	if err := m.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := m.CheckInitialSync(); err != nil {
		t.Fatalf("successful empty fetch confirms defaults: %v", err)
	}
}
