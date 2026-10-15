package rtconfig

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type snapshot struct {
	values     map[string]Value
	generation uint64
	clusterID  uint64
	revision   int64
}

// Manager owns one etcd client and one watch. The mutex protects control-plane
// state only; all successful Value reads use the atomic snapshot directly.
type Manager struct {
	schema  *Schema
	opts    Options
	current atomic.Pointer[snapshot]
	open    func(context.Context) (backend, error)

	mu           sync.Mutex
	status       Status
	started      bool
	closed       bool
	cancel       context.CancelFunc
	done         chan struct{}
	observers    []*observer
	nextObserver int
	notify       chan struct{}
	changed      chan struct{}
}

var _ Client = (*Manager)(nil)

// New validates static settings and publishes defaults without network I/O or
// goroutines. Start explicitly owns the background lifetime.
func New(schema *Schema, opts Options) (*Manager, error) {
	if schema == nil || schema.hash == "" {
		return nil, errors.New("rtconfig: schema must be created with NewSchema or ParseSchema")
	}

	normalized, err := opts.normalized()
	if err != nil {
		return nil, err
	}

	m := &Manager{
		schema: schema,
		opts:   normalized,
		status: Status{
			Source:     "defaults",
			Generation: 1,
		},
		done:    make(chan struct{}),
		notify:  make(chan struct{}, 1),
		changed: make(chan struct{}),
	}
	m.current.Store(&snapshot{
		values:     schema.defaults(),
		generation: 1,
	})
	m.open = func(ctx context.Context) (backend, error) {
		return openEtcd(ctx, normalized)
	}

	return m, nil
}

// Start runs until ctx is cancelled or Close is called. It waits for the first
// bounded fetch attempt, not for etcd availability. Offline startup keeps defaults
// and retries in the background. Pass an application lifetime context here.
func (m *Manager) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	if m.started {
		m.mu.Unlock()
		return ErrAlreadyStarted
	}

	lifetime, cancel := context.WithCancel(ctx)
	m.started = true
	m.cancel = cancel
	m.mu.Unlock()

	first := make(chan error, 1)
	workerDone := make(chan struct{})
	dispatchDone := make(chan struct{})

	go func() {
		defer close(workerDone)
		m.run(lifetime, first)
	}()
	go func() {
		defer close(dispatchDone)
		m.dispatch(lifetime)
	}()
	go func() {
		<-workerDone
		cancel()
		<-dispatchDone

		m.mu.Lock()
		m.closed = true
		m.status.Closed = true
		m.status.Connected = false
		m.signalChangedLocked()
		close(m.done)
		m.mu.Unlock()
	}()

	initial := time.NewTimer(m.opts.StartupTimeout)
	defer initial.Stop()
	select {
	case err := <-first:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-initial.C:
		// This limits the complete open/authenticate/Get sequence, without
		// cancelling the background lifetime needed for later recovery.
		return nil
	}
}

// Close is idempotent and bounded by ctx. It cancels network activity before
// waiting for workers. A callback that never returns can exhaust this budget;
// spawning another goroutine per callback would only conceal that leak.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		m.status.Closed = true
		m.status.Connected = false
		if m.cancel != nil {
			m.cancel()
		}
		if !m.started {
			close(m.done)
		}
		m.signalChangedLocked()
	}
	m.mu.Unlock()

	select {
	case <-m.done:
		return nil
	default:
	}

	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// CheckReady проверяет lifecycle клиента без обращения к etcd.
// После Start валидные defaults или последний snapshot достаточны для работы.
func (m *Manager) CheckReady() error {
	if m == nil {
		return ErrNotInitialized
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrClosed
	}
	if !m.started {
		return ErrNotInitialized
	}
	return nil
}

// Value reads only memory. Request cancellation does not invalidate a ready
// snapshot, and closing Manager does not invalidate already published values.
func (m *Manager) Value(_ context.Context, name string) (Value, error) {
	value, ok := m.current.Load().values[name]
	if !ok {
		return nil, ErrVariableNotFound
	}

	return value, nil
}

// Values copies the map from a single generation. Its immutable values may be
// retained, but callers cannot modify the manager's own map.
func (m *Manager) Values(_ context.Context) (map[string]Value, error) {
	current := m.current.Load()
	values := make(map[string]Value, len(current.values))
	for name, value := range current.values {
		values[name] = value
	}

	return values, nil
}

func (m *Manager) Version() Version {
	current := m.current.Load()
	return version{
		id:      revisionID(current.clusterID, current.generation, current.revision),
		project: m.opts.ProjectID,
	}
}

// Ready means usable defaults or a validated remote snapshot, not fresh etcd.
func (m *Manager) Ready(_ context.Context) <-chan error {
	return ready
}

var ready = func() <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}()

// RequireType checks consumer/schema agreement before serving requests.
func (m *Manager) RequireType(name string, typ Type) error {
	return m.schema.RequireType(name, typ)
}

// Status makes a consistent copy without contacting etcd. Staleness is measured
// from quorum confirmation, not from the last configuration change.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()

	status := m.status
	status.Stale = status.LastConfirmed.IsZero() || time.Since(status.LastConfirmed) > m.opts.StaleAfter
	status.Observers = make([]ObserverStatus, len(m.observers))
	for i, observer := range m.observers {
		status.Observers[i] = observer.status
	}

	return status
}

// apply publishes a complete candidate. Keeping raw remote state outside the
// effective snapshot lets a later correction repair a rejected transaction.
func (m *Manager) apply(raw map[string][]byte, clusterID uint64, revision int64) {
	values, err := m.schema.decode(raw)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}

	m.status.ClusterID = clusterID
	m.status.ObservedRevision = revision
	if err != nil {
		m.status.Invalid = true
		m.status.LastError = diagnostic(err)
		return
	}

	old := m.current.Load()
	next := &snapshot{
		values:     values,
		generation: old.generation + 1,
		clusterID:  clusterID,
		revision:   revision,
	}
	m.current.Store(next)
	m.status.Source = "etcd"
	m.status.Invalid = false
	m.status.LastError = ""
	m.status.AppliedRevision = revision
	m.status.AppliedClusterID = clusterID
	m.status.Generation = next.generation

	for _, observer := range m.observers {
		if snapshotChanged(old, next, observer.names) {
			observer.pending = next
			observer.status.PendingGeneration = next.generation
		}
	}
	m.wakeDispatcherLocked()
}

func (m *Manager) observed(revision int64) {
	m.mu.Lock()
	m.status.ObservedRevision = revision
	m.mu.Unlock()
}

func (m *Manager) confirm(at time.Time) {
	m.mu.Lock()
	if !m.closed && at.After(m.status.LastConfirmed) {
		m.status.LastConfirmed = at
	}
	m.mu.Unlock()
}

func (m *Manager) connected(value bool) {
	m.mu.Lock()
	m.status.Connected = value && !m.closed
	m.mu.Unlock()
}

func (m *Manager) failed(err error) {
	m.mu.Lock()
	m.status.Connected = false
	m.status.LastError = diagnostic(err)
	if errors.Is(err, ErrSnapshotLimit) {
		m.status.Invalid = true
	}
	m.mu.Unlock()
}

func diagnostic(err error) string {
	// Network errors may contain credentials/endpoints; callers supply safe error
	// classes. Keep even locally constructed diagnostics bounded.
	message := fmt.Sprint(err)
	if len(message) > 256 {
		return message[:256]
	}

	return message
}

func snapshotChanged(old, next *snapshot, names []string) bool {
	if old == nil {
		return true
	}

	for _, name := range names {
		if old.values[name].String() != next.values[name].String() {
			return true
		}
	}

	return false
}
