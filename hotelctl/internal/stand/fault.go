package stand

import (
	"context"
	"fmt"
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/diagnostics"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/fault"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/kube"
)

func (m *Manager) Fault(ctx context.Context, options fault.Options) error {
	if err := options.Validate(); err != nil {
		return err
	}
	unlock, err := m.Store.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	state, err := m.running(ctx)
	if err != nil {
		return err
	}
	if state.Reset != nil {
		return fmt.Errorf("сначала завершите hotelctl reset --yes")
	}
	edge, err := fault.FindEdge(options.From, options.To)
	if err != nil {
		return err
	}
	services, err := m.Config.Select([]string{edge.From, edge.To})
	if err != nil {
		return err
	}
	client, err := kube.New(m.Config.Kubeconfig())
	if err != nil {
		return err
	}
	if err := client.CheckEnvoyRoute(ctx, services[0], edge); err != nil {
		return err
	}
	forward, err := client.ForwardEnvoy(ctx, []int{fault.ControlPort, edge.Port}, m.Streams.ErrOut)
	if err != nil {
		return err
	}
	defer forward.Close()
	control := fault.NewRuntimeClient(fmt.Sprintf("http://127.0.0.1:%d", forward.Ports[fault.ControlPort]))
	defer control.Close()
	address := fmt.Sprintf("127.0.0.1:%d", forward.Ports[edge.Port])
	probe := func() error {
		attempt, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		return diagnostics.CheckEcho(attempt, address, services[1].EchoMethod, edge.To)
	}
	if !options.Clear {
		if err := probe(); err != nil {
			return fmt.Errorf("baseline Envoy → %s: %w", edge.To, err)
		}
	}
	if err := fault.RunRuntime(ctx, control, options, m.Streams.Out); err != nil {
		return err
	}
	if err := probe(); err != nil {
		return fmt.Errorf("правило снято, но контрольный Echo через Envoy → %s не прошёл: %w", edge.To, err)
	}
	fmt.Fprintf(m.Streams.Out, "OK Envoy → %s: контрольный Echo после снятия правила\n", edge.To)
	return nil
}
