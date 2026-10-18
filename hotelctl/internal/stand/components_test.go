package stand

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/project"
)

type componentFake struct {
	counts   map[string]int32
	stopped  []string
	ready    []string
	scaleErr error
	readErr  error
}

func (f *componentFake) Replicas(_ context.Context, components []config.Service) ([]project.ServiceReplicas, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	var result []project.ServiceReplicas
	for _, component := range components {
		result = append(result, project.ServiceReplicas{
			Service:  component,
			Replicas: f.counts[component.Name],
		})
	}
	return result, nil
}

func (f *componentFake) Scale(_ context.Context, component config.Service, replicas int32) error {
	if f.scaleErr != nil {
		return f.scaleErr
	}
	f.counts[component.Name] = replicas
	return nil
}

func (f *componentFake) WaitStopped(_ context.Context, component config.Service) error {
	f.stopped = append(f.stopped, component.Name)
	return nil
}

func (f *componentFake) WaitDeployment(_ context.Context, component config.Service) error {
	f.ready = append(f.ready, component.Name)
	return nil
}

func TestComponentStopStartPreservesReplicasAcrossCommands(t *testing.T) {
	m := &Manager{
		Store:   project.Store{Dir: t.TempDir()},
		Streams: iostreams.IOStreams{Out: io.Discard},
	}
	state := &project.State{
		Version: 1,
		Owner:   "test",
		Name:    "hotelflow",
	}
	components := []config.Service{{Name: "booking"}, {Name: "kafka"}}
	f := &componentFake{
		counts: map[string]int32{
			"booking": 3,
			"kafka":   1,
		},
	}
	for range 2 {
		if err := m.control(context.Background(), state, f, "stop", components); err != nil {
			t.Fatal(err)
		}
		var err error
		state, err = m.Store.Load()
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.counts["booking"] != 0 || state.StoppedComponents["booking"] != 3 {
		t.Fatalf("stop lost original replicas: %+v / %+v", f.counts, state.StoppedComponents)
	}
	if err := m.control(context.Background(), state, f, "start", components); err != nil {
		t.Fatal(err)
	}
	if f.counts["booking"] != 3 || f.counts["kafka"] != 1 || len(f.ready) != 2 || len(state.StoppedComponents) != 0 {
		t.Fatalf("incorrect restoration: %+v / %+v", f, state)
	}
}

func TestComponentFailureKeepsRecoveryState(t *testing.T) {
	m := &Manager{
		Store:   project.Store{Dir: t.TempDir()},
		Streams: iostreams.IOStreams{Out: io.Discard},
	}
	state := &project.State{
		Version: 1,
		Owner:   "test",
		Name:    "hotelflow",
	}
	f := &componentFake{
		counts:   map[string]int32{"booking": 2},
		scaleErr: errors.New("API disconnected"),
	}
	err := m.control(context.Background(), state, f, "stop", []config.Service{{Name: "booking"}})
	if !errors.Is(err, f.scaleErr) {
		t.Fatalf("lost API error: %v", err)
	}
	saved, err := m.Store.Load()
	if err != nil || saved.StoppedComponents["booking"] != 2 || len(f.stopped) != 0 {
		t.Fatalf("recovery state not durable: %+v, %v", saved, err)
	}
}

func TestComponentRestartAndPreflight(t *testing.T) {
	for _, test := range []struct {
		name    string
		count   int32
		reset   bool
		readErr error
		wantErr bool
	}{
		{
			name:  "restart running",
			count: 2,
		},
		{
			name:    "reject stopped",
			count:   0,
			wantErr: true,
		},
		{
			name:    "unfinished reset",
			count:   1,
			reset:   true,
			wantErr: true,
		},
		{
			name:    "deployment unavailable",
			count:   1,
			readErr: errors.New("missing"),
			wantErr: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := &Manager{
				Store:   project.Store{Dir: t.TempDir()},
				Streams: iostreams.IOStreams{Out: io.Discard},
			}
			state := &project.State{}
			if test.reset {
				state.Reset = &project.ResetState{}
			}
			f := &componentFake{
				counts:  map[string]int32{"kafka": test.count},
				readErr: test.readErr,
			}
			err := m.control(context.Background(), state, f, "restart", []config.Service{{Name: "kafka"}})
			if (err != nil) != test.wantErr || f.counts["kafka"] != test.count {
				t.Fatalf("unexpected outcome: %v, %+v", err, f)
			}
			if test.wantErr && len(f.stopped) != 0 {
				t.Fatal("preflight failure mutated components")
			}
			if !test.wantErr && (len(f.stopped) != 1 || len(f.ready) != 1) {
				t.Fatal("restart did not wait for stop/readiness")
			}
		})
	}
}
