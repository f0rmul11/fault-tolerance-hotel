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
	"google.golang.org/grpc/metadata"
)

func flagEvent(cluster uint64, eventRevision, headerRevision int64, value string) clientv3.WatchResponse {
	return clientv3.WatchResponse{
		Header: etcdserverpb.ResponseHeader{
			ClusterId: cluster,
			Revision:  headerRevision,
		},
		Events: []*clientv3.Event{{
			Type: clientv3.EventTypePut,
			Kv: &mvccpb.KeyValue{
				Key:         []byte("/test/flag"),
				Value:       []byte(value),
				ModRevision: eventRevision,
			},
		}},
	}
}

func TestWatchUsesEventRevisionAndSkipsCreatedHeader(t *testing.T) {
	manager := newTestManager(t)
	defer closeManager(t, manager)
	updates := make(chan clientv3.WatchResponse, 3)
	updates <- clientv3.WatchResponse{
		Header: etcdserverpb.ResponseHeader{
			ClusterId: 1,
			Revision:  100,
		},
		Created: true,
	}
	updates <- flagEvent(1, 2, 100, "true")
	updates <- flagEvent(1, 3, 100, "false")
	close(updates)
	store := &fakeBackend{
		watch: func(ctx context.Context, key string, opts ...clientv3.OpOption) clientv3.WatchChan {
			if clientv3.OpGet(key, opts...).Rev() != 2 {
				t.Error("watch did not start after the snapshot revision")
			}
			if md, ok := metadata.FromOutgoingContext(ctx); !ok || len(md) == 0 {
				t.Error("watch is missing require-leader metadata")
			}
			return updates
		},
	}

	err := manager.watch(context.Background(), store, map[string][]byte{}, 1, 1)
	if !errors.Is(err, errWatchClosed) {
		t.Fatal(err)
	}
	if got := manager.Status(); got.ObservedRevision != 3 || got.AppliedRevision != 3 {
		t.Fatalf("historical header advanced cursor: %+v", got)
	}
	value, _ := manager.Value(context.Background(), "flag")
	if value.Bool() {
		t.Fatal("second historical revision was skipped")
	}
}

func TestForeignClusterDeltaIsNeverApplied(t *testing.T) {
	manager := newTestManager(t)
	defer closeManager(t, manager)
	raw := map[string][]byte{"flag": []byte("false")}
	manager.apply(raw, 1, 10)
	updates := make(chan clientv3.WatchResponse, 1)
	updates <- flagEvent(2, 20, 20, "true")
	store := &fakeBackend{
		watch: func(context.Context, string, ...clientv3.OpOption) clientv3.WatchChan {
			return updates
		},
	}

	if err := manager.watch(context.Background(), store, raw, 1, 10); !errors.Is(err, errClusterChanged) {
		t.Fatalf("foreign cluster: %v", err)
	}
	value, _ := manager.Value(context.Background(), "flag")
	if value.Bool() || string(raw["flag"]) != "false" {
		t.Fatal("foreign cluster event contaminated existing state")
	}
}

func TestWatchRegistrationHasIndependentDeadline(t *testing.T) {
	manager := newTestManager(t)
	manager.opts.WatchTimeout = 10 * time.Millisecond
	defer closeManager(t, manager)
	store := &fakeBackend{
		watch: func(ctx context.Context, _ string, _ ...clientv3.OpOption) clientv3.WatchChan {
			<-ctx.Done()
			return nil
		},
	}

	err := manager.watch(context.Background(), store, nil, 1, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unbounded registration: %v", err)
	}
}

func TestWatchEventOvertakingProbeIsNotRollback(t *testing.T) {
	manager := newTestManager(t)
	manager.opts.ProbeInterval = time.Millisecond
	manager.opts.ProbeTimeout = 100 * time.Millisecond
	defer closeManager(t, manager)
	raw := map[string][]byte{"flag": []byte("false")}
	manager.apply(raw, 1, 100)
	updates := make(chan clientv3.WatchResponse)
	probeStarted := make(chan struct{})
	releaseProbe := make(chan struct{})
	nextProbe := make(chan struct{})
	var gets atomic.Int32
	store := &fakeBackend{
		watch: func(context.Context, string, ...clientv3.OpOption) clientv3.WatchChan {
			return updates
		},
		get: func(ctx context.Context, _ string, _ ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			if gets.Add(1) == 1 {
				close(probeStarted)
				select {
				case <-releaseProbe:
					return getResponse(1, 100, nil), nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}

			close(nextProbe)
			<-ctx.Done()
			return nil, ctx.Err()
		},
		progress: func(ctx context.Context) error {
			if md, ok := metadata.FromOutgoingContext(ctx); !ok || len(md) == 0 {
				t.Error("progress metadata differs from require-leader watch")
			}
			return nil
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		finished <- manager.watch(ctx, store, raw, 1, 100)
	}()

	select {
	case <-probeStarted:
	case <-ctx.Done():
		t.Fatal("probe did not start")
	}
	select {
	case updates <- flagEvent(1, 101, 101, "true"):
	case <-ctx.Done():
		t.Fatal("watch did not consume update")
	}
	// Sending a second message synchronizes with completion of the first case.
	select {
	case updates <- clientv3.WatchResponse{
		Header: etcdserverpb.ResponseHeader{
			ClusterId: 1,
			Revision:  101,
		},
		Created: true,
	}:
	case <-ctx.Done():
		t.Fatal("watch did not apply update")
	}
	close(releaseProbe)

	select {
	case err := <-finished:
		t.Fatalf("overtaking event was treated as rollback: %v", err)
	case <-nextProbe:
	case <-ctx.Done():
		t.Fatal("probe result was not acknowledged")
	}
	if manager.Status().LastConfirmed.IsZero() {
		t.Fatal("quorum confirmation was not recorded")
	}
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestInvalidCandidateAdvancesCursorUntilCorrection(t *testing.T) {
	manager := newTestManager(t)
	defer closeManager(t, manager)
	raw := map[string][]byte{}
	cursor, err := manager.applyEvents(raw, 1, 1, flagEvent(1, 2, 100, `"wrong"`).Events)
	if err != nil || cursor != 2 {
		t.Fatalf("poison event did not advance cursor: %d, %v", cursor, err)
	}
	if state := manager.Status(); !state.Invalid || state.AppliedRevision != 0 {
		t.Fatalf("invalid candidate was published: %+v", state)
	}

	cursor, err = manager.applyEvents(raw, 1, cursor, flagEvent(1, 3, 100, "true").Events)
	if err != nil || cursor != 3 || manager.Status().Invalid {
		t.Fatalf("correction failed: %d, %v, %+v", cursor, err, manager.Status())
	}
	value, _ := manager.Value(context.Background(), "flag")
	if !value.Bool() {
		t.Fatal("corrected value was not published")
	}
}

func TestPrefixLimitsPreserveLastGood(t *testing.T) {
	manager := newTestManager(t)
	defer closeManager(t, manager)
	manager.apply(map[string][]byte{"flag": []byte("true")}, 1, 2)
	store := &fakeBackend{
		get: func(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
			response := getResponse(1, 3, nil)
			response.Count = maxKeys + 1
			response.More = true
			return response, nil
		},
	}
	if _, _, _, err := manager.fetch(context.Background(), store); !errors.Is(err, ErrSnapshotLimit) {
		t.Fatalf("truncated prefix accepted: %v", err)
	}
	value, _ := manager.Value(context.Background(), "flag")
	if !value.Bool() {
		t.Fatal("oversize prefix replaced last good snapshot")
	}
}
