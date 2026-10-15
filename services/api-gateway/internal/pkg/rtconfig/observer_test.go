package rtconfig

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type observedValue struct {
	old  Variable
	next Variable
}

// observerDispatcher exercises the real dispatcher without starting an etcd worker.
// A start channel permits registration and publication before initial dispatch.
func observerDispatcher(t *testing.T, m *Manager, start <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.started = true
	m.cancel = cancel
	m.mu.Unlock()

	go func() {
		if start != nil {
			select {
			case <-start:
			case <-ctx.Done():
			}
		}
		m.dispatch(ctx)

		m.mu.Lock()
		m.closed = true
		m.status.Closed = true
		m.signalChangedLocked()
		close(m.done)
		m.mu.Unlock()
	}()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Errorf("dispatcher shutdown: %v", err)
		}
	})
}

func receiveObservedValue(t *testing.T, values <-chan observedValue) observedValue {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("callback was not delivered")
		return observedValue{}
	}
}

func awaitObserverStatus(t *testing.T, m *Manager, ready func(Status) bool) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		m.mu.Lock()
		changed := m.changed
		m.mu.Unlock()
		if ready(m.Status()) {
			return
		}
		select {
		case <-changed:
		case <-timer.C:
			t.Fatal("observer did not reach the expected state")
		}
	}
}

func TestObserverInitialValueAndRegistrationContext(t *testing.T) {
	m := newTestManager(t)
	observerDispatcher(t, m, nil)
	values := make(chan observedValue, 3)
	callback := func(old, next Variable) {
		values <- observedValue{
			old:  old,
			next: next,
		}
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.WatchVariable(canceled, "flag", callback); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled registration = %v", err)
	}
	if len(m.Status().Observers) != 0 {
		t.Fatal("failed registration left a subscription")
	}

	registration, cancel := context.WithCancel(context.Background())
	if err := m.WatchVariable(registration, "flag", callback); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	initial := receiveObservedValue(t, values)
	if initial.old.Name() != "flag" || initial.next.Name() != "flag" {
		t.Fatal("initial variables must both retain their key name")
	}
	if !errors.Is(initial.old.Value().Err(), ErrNoPreviousValue) || initial.old.Value().Bool() || initial.next.Value().Err() != nil || initial.next.Value().Bool() {
		t.Fatal("initial callback must contain an error Value and current default")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.WaitForInitial(ctx); err != nil {
		t.Fatal(err)
	}

	m.apply(map[string][]byte{"flag": []byte("true")}, 1, 2)
	updated := receiveObservedValue(t, values)
	if updated.old.Value().Err() != nil || updated.old.Value().Bool() || !updated.next.Value().Bool() {
		t.Fatal("canceling a completed registration removed or corrupted its observer")
	}
}

func TestObserverCoalescesUpdatesWithoutBlockingReads(t *testing.T) {
	m := newTestManager(t)
	start := make(chan struct{})
	observerDispatcher(t, m, start)
	initialStarted := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(release)
		})
	})
	updates := make(chan observedValue, 3)
	if err := m.WatchVariable(context.Background(), "flag", func(old, next Variable) {
		if errors.Is(old.Value().Err(), ErrNoPreviousValue) {
			close(initialStarted)
			<-release
			return
		}
		updates <- observedValue{
			old:  old,
			next: next,
		}
	}); err != nil {
		t.Fatal(err)
	}

	// Publication before initial dispatch must not replace the captured default.
	m.apply(map[string][]byte{"flag": []byte("true")}, 1, 2)
	close(start)
	select {
	case <-initialStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("initial callback did not start")
	}
	status := m.Status()
	if len(status.Observers) != 1 || status.Observers[0].InitialGeneration != 1 || status.Observers[0].PendingGeneration != 2 || !status.Observers[0].Running {
		t.Fatalf("initial callback hides newer pending state: %+v", status.Observers)
	}

	published := make(chan struct{})
	go func() {
		m.apply(map[string][]byte{"flag": []byte("false")}, 1, 3)
		m.apply(map[string][]byte{"flag": []byte("true")}, 1, 4)
		close(published)
	}()
	select {
	case <-published:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked callback prevented snapshot publication")
	}

	read := make(chan bool, 1)
	go func() {
		value, err := m.Value(context.Background(), "flag")
		read <- err == nil && value.Bool()
	}()
	select {
	case latest := <-read:
		if !latest {
			t.Fatal("reader did not observe the latest published state")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked callback prevented a local read")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.WaitForInitial(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("pending initial wait ignored cancellation: %v", err)
	}
	status = m.Status()
	if status.Generation != 4 || status.Observers[0].CompletedGeneration != 0 || status.Observers[0].PendingGeneration != 4 {
		t.Fatalf("pending state = %+v", status)
	}

	releaseOnce.Do(func() {
		close(release)
	})
	updated := receiveObservedValue(t, updates)
	if updated.old.Value().Bool() || !updated.next.Value().Bool() {
		t.Fatal("coalesced update must compare last delivered false with latest true")
	}
	awaitObserverStatus(t, m, func(status Status) bool {
		return status.Observers[0].CompletedGeneration == 4 && !status.Observers[0].Running
	})
	if len(updates) != 0 {
		t.Fatal("intermediate states were delivered despite coalescing while blocked")
	}
}

func TestObserverPanicIsIsolatedAndInitialWaitReportsFailure(t *testing.T) {
	m := newTestManager(t)
	observerDispatcher(t, m, nil)
	if err := m.WatchVariable(context.Background(), "flag", func(_, _ Variable) {
		panic("private callback payload")
	}); err != nil {
		t.Fatal(err)
	}
	values := make(chan observedValue, 2)
	if err := m.WatchVariable(context.Background(), "flag", func(old, next Variable) {
		values <- observedValue{
			old:  old,
			next: next,
		}
	}); err != nil {
		t.Fatal(err)
	}
	receiveObservedValue(t, values)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.WaitForInitial(ctx); !errors.Is(err, ErrCallbackPanic) {
		t.Fatalf("initial callback panic = %v", err)
	}
	status := m.Status()
	if status.Observers[0].CompletedGeneration != 0 || status.Observers[0].LastError == "" || strings.Contains(status.Observers[0].LastError, "private callback payload") {
		t.Fatalf("panic diagnostic = %+v", status.Observers[0])
	}
	m.apply(map[string][]byte{"flag": []byte("true")}, 1, 2)
	if value := receiveObservedValue(t, values); !value.next.Value().Bool() {
		t.Fatal("panic prevented later observers from receiving updates")
	}
}

func TestWaitForInitialWakesWhenManagerCloses(t *testing.T) {
	m := newTestManager(t)
	observerDispatcher(t, m, nil)
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(release)
		})
	})
	if err := m.WatchVariable(context.Background(), "flag", func(_, _ Variable) {
		close(entered)
		<-release
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("initial callback did not enter")
	}
	waited := make(chan error, 1)
	go func() {
		waited <- m.WaitForInitial(context.Background())
	}()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Close(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close with a running callback and canceled budget = %v", err)
	}
	select {
	case err := <-waited:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("initial wait after close = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not wake initial waiter")
	}
	if err := m.WatchVariable(context.Background(), "flag", func(_, _ Variable) {}); !errors.Is(err, ErrClosed) {
		t.Fatalf("registration after close = %v", err)
	}
	value, err := m.Value(context.Background(), "flag")
	if err != nil || value.Bool() {
		t.Fatal("Close invalidated the published default")
	}
	releaseOnce.Do(func() {
		close(release)
	})
}

func TestWatchAllStopsRemainingCallbacksAfterClose(t *testing.T) {
	schema, err := NewSchema(
		Definition{
			Name:     "first",
			Type:     BoolType,
			Default:  false,
			Writable: true,
		},
		Definition{
			Name:     "second",
			Type:     BoolType,
			Default:  false,
			Writable: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(schema, Options{
		Endpoints: []string{"http://127.0.0.1:2379"},
		Prefix:    "/test/",
	})
	if err != nil {
		t.Fatal(err)
	}
	observerDispatcher(t, m, nil)
	entered := make(chan string, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(release)
		})
	})
	if err := m.WatchAllVariables(context.Background(), func(_, next Variable) {
		entered <- next.Name()
		if next.Name() == "first" {
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case name := <-entered:
		if name != "first" {
			t.Fatalf("initial notifications must follow sorted keys, got %q", name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first initial callback did not enter")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Close(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close with a blocked callback = %v", err)
	}
	releaseOnce.Do(func() {
		close(release)
	})
	select {
	case <-m.done:
	case <-time.After(5 * time.Second):
		t.Fatal("dispatcher did not stop after callback release")
	}
	select {
	case name := <-entered:
		t.Fatalf("callback %q began after manager cancellation", name)
	default:
	}
	status := m.Status()
	if status.Observers[0].CompletedGeneration != 0 || status.Observers[0].Running || status.Observers[0].LastError == "" {
		t.Fatalf("partially delivered initial batch was marked applied: %+v", status.Observers[0])
	}
	if err := m.WaitForInitial(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("initial barrier must report interrupted batch: %v", err)
	}
}

func TestManagerReadsRetainImmutableSnapshots(t *testing.T) {
	m := newTestManager(t)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	retained, err := m.Value(canceled, "flag")
	if err != nil || retained.Bool() {
		t.Fatal("request cancellation invalidated memory read")
	}
	values, err := m.Values(canceled)
	if err != nil {
		t.Fatal(err)
	}
	delete(values, "flag")
	values["injected"] = NewNilValue(ErrNoPreviousValue)
	m.apply(map[string][]byte{"flag": []byte("true")}, 1, 2)
	current, err := m.Values(context.Background())
	if err != nil || len(current) != 1 || !current["flag"].Bool() || retained.Bool() {
		t.Fatal("caller map or publication mutated a retained snapshot")
	}
	if _, err := m.Value(context.Background(), "injected"); !errors.Is(err, ErrVariableNotFound) {
		t.Fatal("returned Values map escaped into manager state")
	}
	allocations := testing.AllocsPerRun(1000, func() {
		value, err := m.Value(context.Background(), "flag")
		if err != nil || !value.Bool() {
			panic("unexpected memory read")
		}
	})
	if allocations != 0 {
		t.Fatalf("successful read allocated %g objects", allocations)
	}
}

func TestManagerConcurrentReadAndPublication(t *testing.T) {
	m := newTestManager(t)
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for range 2000 {
				value, err := m.Value(context.Background(), "flag")
				if err != nil || value.Err() != nil || value.Type() != BoolType {
					t.Error("reader observed a partial or invalid value")
					return
				}
				values, err := m.Values(context.Background())
				if err != nil || len(values) != 1 || values["flag"].Type() != BoolType {
					t.Error("reader observed a partial values map")
					return
				}
				delete(values, "flag")
			}
		})
	}
	for revision := int64(1); revision <= 1000; revision++ {
		raw := []byte("false")
		if revision%2 == 0 {
			raw = []byte("true")
		}
		m.apply(map[string][]byte{"flag": raw}, 1, revision)
	}
	workers.Wait()
}

func BenchmarkManagerValueParallel(b *testing.B) {
	schema, err := NewSchema(Definition{
		Name:     "flag",
		Type:     BoolType,
		Default:  false,
		Writable: true,
	})
	if err != nil {
		b.Fatal(err)
	}
	m, err := New(schema, Options{
		Endpoints: []string{"http://127.0.0.1:2379"},
		Prefix:    "/test/",
	})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			value, err := m.Value(context.Background(), "flag")
			if err != nil || value.Bool() {
				b.Error("unexpected memory read")
				return
			}
		}
	})
}
