package stand

import (
	"context"
	"fmt"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/kube"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/project"
)

type componentCluster interface {
	Replicas(context.Context, []config.Service) ([]project.ServiceReplicas, error)
	Scale(context.Context, config.Service, int32) error
	WaitStopped(context.Context, config.Service) error
	WaitDeployment(context.Context, config.Service) error
}

func (m *Manager) Control(ctx context.Context, action string, components []config.Service) error {
	unlock, err := m.Store.Lock()
	if err != nil {
		return err
	}
	defer unlock()

	state, err := m.running(ctx)
	if err != nil {
		return err
	}
	client, err := kube.New(m.Config.Kubeconfig())
	if err != nil {
		return err
	}
	return m.control(ctx, state, client, action, components)
}

func (m *Manager) control(ctx context.Context, state *project.State, cluster componentCluster, action string, components []config.Service) error {
	if state.Reset != nil {
		return fmt.Errorf("сброс данных не завершён; выполните hotelctl reset --yes")
	}
	if action != "stop" && action != "start" && action != "restart" {
		return fmt.Errorf("неизвестное действие %q", action)
	}
	if len(components) == 0 {
		return fmt.Errorf("укажите хотя бы один компонент")
	}
	// Resolve every deployment before changing the first component.
	counts, err := cluster.Replicas(ctx, components)
	if err != nil {
		return err
	}
	if action == "restart" {
		for _, count := range counts {
			if count.Replicas == 0 {
				return fmt.Errorf("%s остановлен; выполните hotelctl start %s", count.Service.Name, count.Service.Name)
			}
		}
	}
	if state.StoppedComponents == nil {
		state.StoppedComponents = map[string]int32{}
	}
	if action == "stop" || action == "restart" {
		for _, count := range counts {
			if count.Replicas > 0 {
				state.StoppedComponents[count.Service.Name] = count.Replicas
			}
		}
		// Save restoration counts before the first scale, including partial failures.
		if err := m.Store.Save(state); err != nil {
			return err
		}
		for _, component := range components {
			fmt.Fprintf(m.Streams.Out, "Остановка %s\n", component.Name)
			if err := cluster.Scale(ctx, component, 0); err != nil {
				return err
			}
		}
		for _, component := range components {
			if err := cluster.WaitStopped(ctx, component); err != nil {
				return err
			}
		}
		if action == "stop" {
			fmt.Fprintln(m.Streams.Out, "Компоненты остановлены. Постоянные тома сохранены; память Jaeger и очереди Collector не сохраняются. Запуск: hotelctl start <имя>.")
			return nil
		}
	}
	for _, count := range counts {
		replicas := count.Replicas
		if replicas == 0 {
			replicas = state.StoppedComponents[count.Service.Name]
		}
		if replicas == 0 {
			replicas = 1
		}
		fmt.Fprintf(m.Streams.Out, "Запуск %s: %d реплик\n", count.Service.Name, replicas)
		if err := cluster.Scale(ctx, count.Service, replicas); err != nil {
			return err
		}
	}
	for _, component := range components {
		if err := cluster.WaitDeployment(ctx, component); err != nil {
			return err
		}
		delete(state.StoppedComponents, component.Name)
		if err := m.Store.Save(state); err != nil {
			return err
		}
		fmt.Fprintf(m.Streams.Out, "OK %s готов\n", component.Name)
	}
	return nil
}
