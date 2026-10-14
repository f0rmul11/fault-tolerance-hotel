package stand

import (
	"context"
	"fmt"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/kube"
)

func (m *Manager) Build(ctx context.Context, services []config.Service) error {
	unlock, err := m.Store.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	if err = m.Runtime.Check(ctx); err != nil {
		return err
	}
	state, err := m.load(true)
	if err != nil {
		return err
	}
	if err = m.compatible(state); err != nil {
		return err
	}
	return m.build(ctx, state, services)
}

func (m *Manager) Deploy(ctx context.Context, services []config.Service) error {
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
		return fmt.Errorf("сброс данных не завершён; выполните hotelctl reset --yes")
	}
	client, err := kube.New(m.Config.Kubeconfig())
	if err != nil {
		return err
	}
	return m.deploy(ctx, state, services, client)
}

func (m *Manager) Logs(ctx context.Context, service config.Service, follow bool, tail int64) error {
	if _, err := m.running(ctx); err != nil {
		return err
	}
	client, err := kube.New(m.Config.Kubeconfig())
	if err != nil {
		return err
	}
	return client.Logs(ctx, service, follow, tail, m.Streams.Out)
}

func (m *Manager) Kubectl(ctx context.Context, args []string) error {
	state, err := m.running(ctx)
	if err != nil {
		return err
	}
	command := append([]string{
		"kubectl",
		"--kubeconfig=/etc/rancher/k3s/k3s.yaml",
	}, args...)
	return m.Runtime.Exec(ctx, state, command, m.Streams.In, m.Streams.Out, m.Streams.ErrOut)
}
