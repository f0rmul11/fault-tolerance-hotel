package rtconfig

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
	"google.golang.org/grpc/status"
)

// backend is deliberately small: tests control protocol ordering without
// pretending that a fake proves etcd's server-side guarantees.
type backend interface {
	Get(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error)
	Watch(context.Context, string, ...clientv3.OpOption) clientv3.WatchChan
	RequestProgress(context.Context) error
	Close() error
}

var (
	errWatchClosed      = errors.New("rtconfig: watch channel closed")
	errClusterChanged   = errors.New("rtconfig: etcd cluster identity changed")
	errRevisionRollback = errors.New("rtconfig: etcd revision moved backwards")
	errWatchStalled     = errors.New("rtconfig: watch did not catch up with quorum read")
	errInvalidResponse  = errors.New("rtconfig: invalid etcd response")
)

func openEtcd(ctx context.Context, opts Options) (backend, error) {
	lifetime, cancel := context.WithCancel(ctx)
	initial := time.AfterFunc(opts.RequestTimeout, cancel)
	client, err := clientv3.New(clientv3.Config{
		Endpoints:          opts.Endpoints,
		TLS:                opts.TLS,
		Username:           opts.Username,
		Password:           opts.Password,
		DialTimeout:        opts.RequestTimeout,
		MaxCallRecvMsgSize: maxReceiveBytes,
		Context:            lifetime,
		Logger:             zap.NewNop(),
	})
	if !initial.Stop() {
		cancel()
	}
	if err != nil {
		cancel()
		return nil, err
	}
	if err := lifetime.Err(); err != nil {
		cancel()
		client.Close()
		return nil, err
	}

	return &ownedBackend{
		Client: client,
		cancel: cancel,
	}, nil
}

type ownedBackend struct {
	*clientv3.Client
	cancel context.CancelFunc
}

func (b *ownedBackend) Close() error {
	b.cancel()
	return b.Client.Close()
}

type storeError struct {
	operation string
	cause     error
}

func (e storeError) Error() string {
	switch {
	case errors.Is(e.cause, rpctypes.ErrPermissionDenied):
		return "rtconfig: " + e.operation + " failed (PermissionDenied)"
	case errors.Is(e.cause, rpctypes.ErrAuthFailed), errors.Is(e.cause, rpctypes.ErrInvalidAuthToken):
		return "rtconfig: " + e.operation + " failed (Unauthenticated)"
	case errors.Is(e.cause, rpctypes.ErrCompacted):
		return "rtconfig: " + e.operation + " failed (Compacted)"
	}
	return fmt.Sprintf("rtconfig: %s failed (%s)", e.operation, status.Code(e.cause))
}

func (e storeError) Unwrap() error {
	return e.cause
}

func (m *Manager) run(ctx context.Context, first chan<- error) {
	var store backend
	defer func() {
		if store != nil {
			if err := store.Close(); err != nil && ctx.Err() == nil {
				m.failed(storeError{
					operation: "close etcd",
					cause:     err,
				})
			}
		}
	}()

	initial := true
	backoff := m.opts.RetryMin
	for ctx.Err() == nil {
		started := time.Now()
		if store == nil {
			var err error
			store, err = m.open(ctx)
			if err != nil {
				// Authentication can perform I/O inside clientv3.New. An offline
				// secured cluster must recover just like an offline initial Get.
				m.failed(storeError{
					operation: "open etcd",
					cause:     err,
				})
				if initial {
					first <- nil
					initial = false
				}
				if !pause(ctx, randomDelay(backoff)) {
					break
				}
				backoff = nextBackoff(backoff, m.opts.RetryMax)
				continue
			}
		}
		raw, clusterID, revision, err := m.fetch(ctx, store)
		if err == nil {
			m.apply(raw, clusterID, revision)
			m.confirm(started)
		}

		// Availability of the control plane must not turn startup into an
		// unbounded wait. Defaults are already usable, even after a failed Get.
		if initial {
			first <- nil
			initial = false
		}

		if err == nil {
			err = m.watch(ctx, store, raw, clusterID, revision)
		}
		if ctx.Err() != nil {
			break
		}

		m.failed(err)
		if time.Since(started) >= m.opts.ProbeInterval {
			backoff = m.opts.RetryMin
		}
		if !pause(ctx, randomDelay(backoff)) {
			break
		}
		backoff = nextBackoff(backoff, m.opts.RetryMax)
	}

	if initial {
		first <- ctx.Err()
	}
}

func (m *Manager) fetch(ctx context.Context, store backend) (map[string][]byte, uint64, int64, error) {
	request, cancel := context.WithTimeout(ctx, m.opts.RequestTimeout)
	defer cancel()

	response, err := store.Get(request, m.opts.Prefix,
		clientv3.WithPrefix(), clientv3.WithLimit(maxKeys+1),
	)
	if err != nil {
		return nil, 0, 0, storeError{
			operation: "snapshot",
			cause:     err,
		}
	}
	if response == nil || response.Header == nil || response.Header.ClusterId == 0 || response.Header.Revision <= 0 {
		return nil, 0, 0, errInvalidResponse
	}
	if response.More || response.Count > maxKeys || len(response.Kvs) > maxKeys {
		return nil, 0, 0, ErrSnapshotLimit
	}

	raw := make(map[string][]byte, len(response.Kvs))
	for _, kv := range response.Kvs {
		if kv == nil || !strings.HasPrefix(string(kv.Key), m.opts.Prefix) {
			return nil, 0, 0, errInvalidResponse
		}
		name := strings.TrimPrefix(string(kv.Key), m.opts.Prefix)
		raw[name] = append([]byte(nil), kv.Value...)
	}
	if err := checkRawLimits(raw); err != nil {
		return nil, 0, 0, err
	}

	return raw, response.Header.ClusterId, response.Header.Revision, nil
}

type probeResult struct {
	before    int64
	revision  int64
	clusterID uint64
	at        time.Time
	err       error
}

func (m *Manager) watch(ctx context.Context, store backend, raw map[string][]byte, clusterID uint64, cursor int64) error {
	watchCtx, cancel := context.WithCancel(clientv3.WithRequireLeader(ctx))
	defer cancel()

	// Watch itself waits for server registration. A separate timer bounds that
	// wait without expiring a successfully established long-lived subscription.
	establish := time.AfterFunc(m.opts.WatchTimeout, cancel)
	updates := store.Watch(watchCtx, m.opts.Prefix,
		clientv3.WithPrefix(), clientv3.WithRev(cursor+1), clientv3.WithProgressNotify(),
	)
	if !establish.Stop() {
		cancel()
	}
	if err := watchCtx.Err(); err != nil {
		return storeError{
			operation: "watch registration",
			cause:     err,
		}
	}
	m.connected(true)

	probes := make(chan probeResult, 1)
	ack := make(chan struct{}, 1)
	probeDone := make(chan struct{})
	go func() {
		defer close(probeDone)
		m.probe(watchCtx, store, probes, ack)
	}()
	defer func() {
		cancel()
		<-probeDone
	}()

	catchup := time.NewTimer(m.opts.ProbeTimeout)
	catchup.Stop()
	defer catchup.Stop()
	var pending *probeResult
	var deadline <-chan time.Time

	confirm := func() {
		if pending != nil && cursor >= pending.revision {
			m.confirm(pending.at)
			pending = nil
			catchup.Stop()
			deadline = nil
			ack <- struct{}{}
		}
	}

	for {
		select {
		case <-watchCtx.Done():
			return watchCtx.Err()
		case <-deadline:
			return errWatchStalled
		case result := <-probes:
			if result.err != nil {
				return result.err
			}
			if result.clusterID != clusterID {
				return errClusterChanged
			}
			// A newer watch event may legitimately overtake the Get response.
			// Only the cursor captured BEFORE that RPC is a rollback watermark.
			if result.revision < result.before {
				return errRevisionRollback
			}
			pending = &result
			catchup.Reset(m.opts.ProbeTimeout)
			deadline = catchup.C
			confirm()
		case response, ok := <-updates:
			if !ok {
				return errWatchClosed
			}
			if err := response.Err(); err != nil {
				return storeError{
					operation: "watch",
					cause:     err,
				}
			}
			if response.Canceled || response.CompactRevision != 0 {
				return errWatchClosed
			}
			if response.Header.ClusterId != clusterID {
				return errClusterChanged
			}
			if response.Created {
				continue
			}
			if response.IsProgressNotify() {
				if response.Header.Revision > cursor {
					cursor = response.Header.Revision
					m.observed(cursor)
				}
				confirm()
				continue
			}

			var err error
			cursor, err = m.applyEvents(raw, clusterID, cursor, response.Events)
			if err != nil {
				return err
			}
			confirm()
		}
	}
}

// probe waits for acknowledgement before starting another check. Both Get and
// RequestProgress inherit the watch's outgoing metadata, including require-leader.
func (m *Manager) probe(ctx context.Context, store backend, results chan<- probeResult, ack <-chan struct{}) {
	for pause(ctx, jitteredInterval(m.opts.ProbeInterval)) {
		result := probeResult{
			before: m.Status().ObservedRevision,
			at:     time.Now(),
		}

		request, cancel := context.WithTimeout(ctx, m.opts.RequestTimeout)
		response, err := store.Get(request, m.opts.Prefix,
			clientv3.WithPrefix(), clientv3.WithKeysOnly(), clientv3.WithLimit(1),
		)
		cancel()
		if err != nil {
			result.err = storeError{
				operation: "freshness probe",
				cause:     err,
			}
		} else if response == nil || response.Header == nil {
			result.err = errInvalidResponse
		} else {
			result.clusterID = response.Header.ClusterId
			result.revision = response.Header.Revision
			progress, stop := context.WithTimeout(ctx, m.opts.RequestTimeout)
			err = store.RequestProgress(progress)
			stop()
			if err != nil {
				result.err = storeError{
					operation: "watch progress",
					cause:     err,
				}
			}
		}

		select {
		case results <- result:
		case <-ctx.Done():
			return
		}

		select {
		case <-ack:
		case <-ctx.Done():
			return
		}
	}
}

func (m *Manager) applyEvents(raw map[string][]byte, clusterID uint64, cursor int64, events []*clientv3.Event) (int64, error) {
	for start := 0; start < len(events); {
		if events[start] == nil || events[start].Kv == nil {
			return cursor, errInvalidResponse
		}
		revision := events[start].Kv.ModRevision
		end := start
		for end < len(events) && events[end] != nil && events[end].Kv != nil && events[end].Kv.ModRevision == revision {
			end++
		}
		if revision <= cursor {
			start = end
			continue
		}

		// A transaction's events share a revision and must become visible together.
		for _, event := range events[start:end] {
			key := string(event.Kv.Key)
			if !strings.HasPrefix(key, m.opts.Prefix) {
				return cursor, errInvalidResponse
			}
			name := strings.TrimPrefix(key, m.opts.Prefix)
			switch event.Type {
			case clientv3.EventTypePut:
				if len(name) > maxKeyBytes || len(event.Kv.Value) > maxValueBytes {
					return cursor, ErrSnapshotLimit
				}
				raw[name] = append([]byte(nil), event.Kv.Value...)
			case clientv3.EventTypeDelete:
				delete(raw, name)
			default:
				return cursor, errInvalidResponse
			}
		}
		if err := checkRawLimits(raw); err != nil {
			return cursor, err
		}
		m.apply(raw, clusterID, revision)
		cursor = revision
		start = end
	}

	return cursor, nil
}

func checkRawLimits(raw map[string][]byte) error {
	if len(raw) > maxKeys {
		return ErrSnapshotLimit
	}

	size := 0
	for name, value := range raw {
		if len(name) > maxKeyBytes || len(value) > maxValueBytes {
			return ErrSnapshotLimit
		}
		size += len(name) + len(value)
	}
	if size > maxSnapshotBytes {
		return ErrSnapshotLimit
	}

	return nil
}

func pause(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func randomDelay(limit time.Duration) time.Duration {
	return time.Duration(rand.Int64N(int64(limit)))
}

func nextBackoff(current, maximum time.Duration) time.Duration {
	if current >= maximum/2 {
		return maximum
	}

	return current * 2
}

func jitteredInterval(interval time.Duration) time.Duration {
	spread := interval / 5
	if spread == 0 {
		return interval
	}

	return interval - spread + time.Duration(rand.Int64N(int64(spread)*2))
}
