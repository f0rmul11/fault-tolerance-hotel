package stand

import (
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/project"
)

// Runtime is the boundary between course workflows and their local execution environment.
type Runtime interface {
	Close() error
	Check(context.Context) error
	Ensure(context.Context, *project.State, io.Writer) error
	Kubeconfig(context.Context, *project.State) ([]byte, error)
	Status(context.Context, *project.State) (project.RuntimeStatus, error)
	Build(context.Context, string, config.Service, io.Writer) (string, error)
	Import(context.Context, *project.State, string, string, io.Writer) error
	Stop(context.Context, *project.State) error
	Destroy(context.Context, *project.State) error
	Exec(context.Context, *project.State, []string, io.Reader, io.Writer, io.Writer) error
	Logs(context.Context, *project.State, io.Writer) error
}

// Interface is consumed by commands; implementations never depend on Cobra.
type Interface interface {
	Close() error
	Up(context.Context, []config.Service) error
	Build(context.Context, []config.Service) error
	Deploy(context.Context, []config.Service) error
	Status(context.Context, string) error
	Doctor(context.Context) error
	Smoke(context.Context) error
	Logs(context.Context, config.Service, bool, int64) error
	Stop(context.Context) error
	Destroy(context.Context) error
	Kubectl(context.Context, []string) error
}

type Manager struct {
	Config  config.Config
	Runtime Runtime
	Store   project.Store
	Streams iostreams.IOStreams
}

func New(c config.Config, runtime Runtime, streams iostreams.IOStreams) *Manager {
	return &Manager{Config: c, Runtime: runtime, Store: project.Store{Dir: c.StateDir()}, Streams: streams}
}

func (m *Manager) Close() error { return m.Runtime.Close() }

func (m *Manager) load(create bool) (*project.State, error) {
	state, err := m.Store.Load()
	if err != nil {
		return nil, err
	}
	if state == nil {
		if !create {
			return nil, fmt.Errorf("stand is not initialized; run hotelctl up")
		}
		state, err = project.NewState(m.Config)
		if err != nil {
			return nil, err
		}
		if err = m.Store.Save(state); err != nil {
			return nil, err
		}
	}
	return state, nil
}

func (m *Manager) compatible(s *project.State) error {
	if s.Name != m.Config.Name || s.Runtime != m.Config.Runtime {
		return fmt.Errorf("runtime configuration differs from the saved stand; restore hotel.yaml or explicitly run hotelctl destroy before recreating it")
	}
	return nil
}

func (m *Manager) running(ctx context.Context) (*project.State, error) {
	s, err := m.load(false)
	if err != nil {
		return nil, err
	}
	if err = m.compatible(s); err != nil {
		return nil, err
	}
	if !slices.Equal(s.PortBindings(), m.Config.PortBindings()) {
		return nil, fmt.Errorf("published service ports changed; run hotelctl up to update the container while preserving cluster data")
	}
	status, err := m.Runtime.Status(ctx, s)
	if err != nil {
		return nil, err
	}
	if !status.Running {
		return nil, fmt.Errorf("stand is stopped; run hotelctl up")
	}
	return s, nil
}
