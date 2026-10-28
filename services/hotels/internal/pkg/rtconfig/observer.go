package rtconfig

import (
	"context"
	"errors"
	"fmt"
)

type observer struct {
	names           []string
	callback        VariableChangeCallback
	initial         *snapshot
	pending         *snapshot
	last            *snapshot
	initialFinished bool
	initialError    error
	status          ObserverStatus
}

type notification struct {
	observer *observer
	old      *snapshot
	next     *snapshot
	initial  bool
}

// WatchVariable atomically registers an observer and captures its initial value.
// ctx limits registration only: after success, cancellation does not unsubscribe.
// The observer lives until Close. Register observers once during application init.
func (m *Manager) WatchVariable(ctx context.Context, name string, callback VariableChangeCallback) error {
	if _, ok := m.current.Load().values[name]; !ok {
		return ErrVariableNotFound
	}

	return m.subscribe(ctx, []string{name}, callback)
}

// WatchAllVariables delivers initial values in name order, then changed values.
// It does not make callback side effects atomic across multiple variables.
func (m *Manager) WatchAllVariables(ctx context.Context, callback VariableChangeCallback) error {
	return m.subscribe(ctx, m.schema.Names(), callback)
}

func (m *Manager) subscribe(ctx context.Context, names []string, callback VariableChangeCallback) error {
	if callback == nil {
		return errors.New("rtconfig: callback is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	if m.closed {
		return ErrClosed
	}
	if !m.started {
		return ErrNotStarted
	}
	if len(m.observers) >= maxObservers {
		return ErrObserverLimit
	}

	initial := m.current.Load()
	m.observers = append(m.observers, &observer{
		names:    names,
		callback: callback,
		initial:  initial,
		status: ObserverStatus{
			InitialGeneration: initial.generation,
			PendingGeneration: initial.generation,
		},
	})
	m.wakeDispatcherLocked()

	return nil
}

// WaitForInitial waits for observers registered before this call. It confirms
// their captured initial generation, not that subsequent side effects have
// caught up with the latest published snapshot. Do not call from a callback.
func (m *Manager) WaitForInitial(ctx context.Context) error {
	m.mu.Lock()
	count := len(m.observers)
	m.mu.Unlock()

	for {
		m.mu.Lock()
		complete := true
		for _, observer := range m.observers[:count] {
			if !observer.initialFinished {
				complete = false
				continue
			}
			if observer.initialError != nil {
				err := observer.initialError
				m.mu.Unlock()
				return fmt.Errorf("rtconfig: initial callback failed: %w", err)
			}
		}

		changed := m.changed
		closed := m.closed
		m.mu.Unlock()
		if complete {
			return nil
		}
		if closed {
			return ErrClosed
		}

		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (m *Manager) dispatch(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.notify:
		}

		for ctx.Err() == nil {
			job, ok := m.nextNotification()
			if !ok {
				break
			}

			var callbackError error
			for _, name := range job.observer.names {
				if err := ctx.Err(); err != nil {
					callbackError = err
					break
				}
				value := job.next.values[name]
				old := NewNilValue(ErrNoPreviousValue)
				if job.old != nil {
					old = job.old.values[name]
					if old.String() == value.String() {
						continue
					}
				}

				err := invokeCallback(job.observer.callback,
					variable{
						name:  name,
						value: old,
					},
					variable{
						name:  name,
						value: value,
					},
				)
				if err != nil {
					callbackError = err
				}
			}

			m.finishNotification(job, callbackError)
		}
	}
}

func (m *Manager) nextNotification() (notification, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return notification{}, false
	}

	// Round-robin selection prevents a rapidly changing key from starving others.
	for offset := 0; offset < len(m.observers); offset++ {
		i := (m.nextObserver + offset) % len(m.observers)
		observer := m.observers[i]
		job := notification{
			observer: observer,
			old:      observer.last,
		}

		switch {
		case observer.initial != nil:
			job.initial = true
			job.next = observer.initial
			observer.initial = nil
		case observer.pending != nil:
			job.next = observer.pending
			observer.pending = nil
		default:
			continue
		}

		observer.status.Running = true
		observer.status.PendingGeneration = job.next.generation
		if observer.pending != nil {
			observer.status.PendingGeneration = observer.pending.generation
		}
		m.nextObserver = (i + 1) % len(m.observers)
		return job, true
	}

	return notification{}, false
}

func (m *Manager) finishNotification(job notification, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	observer := job.observer
	observer.last = job.next
	observer.status.Running = false
	observer.status.PendingGeneration = 0
	if observer.pending != nil {
		observer.status.PendingGeneration = observer.pending.generation
	}
	if job.initial {
		observer.initialFinished = true
		observer.initialError = err
	}

	if err != nil {
		observer.status.LastError = err.Error()
	} else {
		observer.status.LastError = ""
		observer.status.CompletedGeneration = job.next.generation
	}
	m.signalChangedLocked()
}

func (m *Manager) wakeDispatcherLocked() {
	select {
	case m.notify <- struct{}{}:
	default:
	}
}

func (m *Manager) signalChangedLocked() {
	close(m.changed)
	m.changed = make(chan struct{})
}

func invokeCallback(callback VariableChangeCallback, old, next Variable) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrCallbackPanic
		}
	}()

	callback(old, next)
	return nil
}
