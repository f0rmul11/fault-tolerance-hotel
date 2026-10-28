package global

import (
	"context"
	"errors"
	"sync"
	"testing"

	"hotels/internal/pkg/rtconfig"
)

func TestClientBeforeInstallation(t *testing.T) {
	previous := runtimeClient.Load()
	t.Cleanup(func() {
		runtimeClient.Store(previous)
	})
	runtimeClient.Store(nil)

	c := Client()
	v, err := c.Value(context.Background(), "rooms_list_enabled")
	if !errors.Is(err, rtconfig.ErrNotInitialized) || v.Bool() || !errors.Is(v.Err(), rtconfig.ErrNotInitialized) {
		t.Fatalf("uninitialized value: value=%v, error=%v", v, err)
	}
	if c.Version().IsReady() {
		t.Fatal("uninitialized client reported ready")
	}
	ready := c.Ready(context.Background())
	if err := <-ready; !errors.Is(err, rtconfig.ErrNotInitialized) {
		t.Fatalf("Ready error = %v", err)
	}
	if _, ok := <-ready; ok {
		t.Fatal("Ready channel remained open")
	}
}

func TestSetClientRejectsNil(t *testing.T) {
	previous := runtimeClient.Load()
	t.Cleanup(func() {
		runtimeClient.Store(previous)
	})

	var typedNil *notInitializedClient
	for _, client := range []rtconfig.Client{nil, typedNil} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("SetClient accepted a nil client")
				}
			}()

			SetClient(client)
		}()
	}
	if runtimeClient.Load() != previous {
		t.Fatal("rejected SetClient modified installed client")
	}
}

func TestClientPublication(t *testing.T) {
	previous := runtimeClient.Load()
	t.Cleanup(func() {
		runtimeClient.Store(previous)
	})

	first := &installedClient{id: 1}
	second := &installedClient{id: 2}
	SetClient(first)
	var readers sync.WaitGroup
	for range 8 {
		readers.Go(func() {
			for range 1000 {
				client := Client()
				if client != first && client != second {
					t.Error("reader observed an unpublished client")
				}
			}
		})
	}
	for range 1000 {
		SetClient(second)
		SetClient(first)
	}
	readers.Wait()
}

type installedClient struct {
	notInitializedClient
	id int
}
