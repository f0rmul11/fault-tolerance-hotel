package stand

import (
	"context"
	"fmt"
	"io"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/diagnostics"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/kube"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/project"
	"github.com/gofrs/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

type resetCluster interface {
	Replicas(context.Context, []config.Service) ([]project.ServiceReplicas, error)
	Scale(context.Context, config.Service, int32) error
	WaitStopped(context.Context, config.Service) error
	WaitDeployment(context.Context, config.Service) error
	ResetData(context.Context, *corev1.ConfigMap, *batchv1.Job, io.Writer) error
	CleanupReset(context.Context, string) error
}

func (m *Manager) Reset(ctx context.Context) error {
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
	if err := m.reset(ctx, state, client); err != nil {
		return fmt.Errorf("сброс не завершён: %w; после устранения причины повторите hotelctl reset --yes", err)
	}
	var enabled []config.Service
	for _, service := range m.Config.Services {
		// Сброс возвращает исходное число реплик, включая намеренно выключенные сервисы.
		counts, err := client.Replicas(ctx, []config.Service{service})
		if err != nil {
			return err
		}
		for _, count := range counts {
			if count.Replicas > 0 {
				enabled = append(enabled, service)
			}
		}
	}
	if err := diagnostics.Smoke(ctx, m.Config, enabled, m.Streams.Out); err != nil {
		return fmt.Errorf("данные сброшены, но проверка доступности не прошла: %w", err)
	}
	fmt.Fprintln(m.Streams.Out, "OK учебные данные сброшены; seed восстановлен, брони и события отсутствуют")
	return nil
}

func (m *Manager) reset(ctx context.Context, state *project.State, cluster resetCluster) error {
	services, err := m.Config.Select([]string{"api-gateway", "booking", "check-in", "hotels", "review", "notification"})
	if err != nil {
		return err
	}
	if len(services) != len(m.Config.Services) {
		return fmt.Errorf("reset поддерживает только шесть сервисов HotelFlow из текущей спецификации")
	}
	for _, service := range services {
		if service.Namespace != "hotelflow" {
			return fmt.Errorf("reset поддерживает namespace hotelflow, получен %s", service.Namespace)
		}
	}
	name := ""
	if state.Reset != nil {
		name = state.Reset.JobName
	} else {
		id, err := uuid.NewV4()
		if err != nil {
			return err
		}
		name = "data-reset-" + id.String()
	}
	cm, job, err := kube.ResetResources(m.Config.Root, name)
	if err != nil {
		return err
	}
	if state.Reset == nil {
		replicas, err := cluster.Replicas(ctx, services)
		if err != nil {
			return err
		}
		state.Reset = &project.ResetState{
			JobName:  name,
			Services: replicas,
		}
		if err := m.Store.Save(state); err != nil {
			return err
		}
	}
	pending := state.Reset
	if !pending.DataReset {
		fmt.Fprintln(m.Streams.Out, "Остановка приложений перед сбросом учебных данных")
		for _, service := range pending.Services {
			if err := cluster.Scale(ctx, service.Service, 0); err != nil {
				return err
			}
		}
		for _, service := range pending.Services {
			if err := cluster.WaitStopped(ctx, service.Service); err != nil {
				return err
			}
		}
		if err := cluster.ResetData(ctx, cm, job, m.Streams.Out); err != nil {
			return err
		}
		pending.DataReset = true
		if err := m.Store.Save(state); err != nil {
			return err
		}
	}
	fmt.Fprintln(m.Streams.Out, "Восстановление исходного числа реплик")
	// Входящий трафик открывается только после готовности внутренних сервисов.
	for _, gateway := range []bool{false, true} {
		for _, service := range pending.Services {
			if (service.Service.Name == "api-gateway") != gateway {
				continue
			}
			if err := cluster.Scale(ctx, service.Service, service.Replicas); err != nil {
				return err
			}
		}
		for _, service := range pending.Services {
			if (service.Service.Name == "api-gateway") != gateway {
				continue
			}
			if err := cluster.WaitDeployment(ctx, service.Service); err != nil {
				return err
			}
		}
	}
	if err := cluster.CleanupReset(ctx, name); err != nil {
		return err
	}
	state.Reset = nil
	return m.Store.Save(state)
}
