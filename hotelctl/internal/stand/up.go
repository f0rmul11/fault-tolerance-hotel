package stand

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/kube"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/project"
)

func (m *Manager) Up(ctx context.Context, services []config.Service) (err error) {
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
	if state.Reset != nil {
		return fmt.Errorf("сброс данных не завершён; выполните hotelctl reset --yes")
	}
	if err = m.compatible(state); err != nil {
		return err
	}
	defer func() {
		if err != nil && ctx.Err() == nil {
			diagnosticCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			fmt.Fprintln(m.Streams.ErrOut, "Startup failed. Diagnostics:")
			_ = m.Runtime.Logs(diagnosticCtx, state, m.Streams.ErrOut)
			if client, e := kube.New(m.Config.Kubeconfig()); e == nil {
				client.Diagnostics(diagnosticCtx, services, m.Streams.ErrOut)
			}
		}
	}()
	if _, err = kube.RenderObservability(m.Config); err != nil {
		return err
	}
	if m.Config.Observability.Enabled && !slices.Equal(state.PortBindings(), m.Config.PortBindings()) {
		status, statusErr := m.Runtime.Status(ctx, state)
		if statusErr != nil {
			return statusErr
		}
		if status.Running {
			client, clientErr := kube.New(m.Config.Kubeconfig())
			if clientErr != nil {
				return clientErr
			}
			if err = client.CheckObservabilityPorts(ctx); err != nil {
				return err
			}
		}
	}
	state.Bindings = m.Config.PortBindings()
	fmt.Fprintln(m.Streams.Out, "Starting one k3s node")
	if err = m.Runtime.Ensure(ctx, state, m.Streams.Out); err != nil {
		return err
	}
	// Save the new bindings only after reconciliation succeeds. A failed
	// preflight must not hide a pending port update on the next attempt.
	if err = m.Store.Save(state); err != nil {
		return err
	}
	data, err := m.Runtime.Kubeconfig(ctx, state)
	if err != nil {
		return err
	}
	if err = kube.WriteConfig(data, m.Config.Kubeconfig(), state.Runtime.Ports.API); err != nil {
		return err
	}
	client, err := kube.New(m.Config.Kubeconfig())
	if err != nil {
		return err
	}
	if err = client.WaitNode(ctx); err != nil {
		return err
	}
	if err = m.build(ctx, state, services); err != nil {
		return err
	}
	if err = m.deploy(ctx, state, services, client); err != nil {
		return err
	}
	fmt.Fprintln(m.Streams.Out, "Стенд запущен; сервисы готовы")
	m.addresses()
	fmt.Fprintln(m.Streams.Out, "Kafka UI: http://localhost:8090\nБД и etcd опубликованы локально: hotelctl access")
	return nil
}

func (m *Manager) addresses() {
	if m.Config.Observability.Enabled {
		fmt.Fprintln(m.Streams.Out, "Jaeger: http://localhost:16686")
		fmt.Fprintln(m.Streams.Out, "Grafana: http://localhost:3000 (логин admin; пароль в Secret hotelflow/grafana-admin)")
	}
	for _, service := range m.Config.Services {
		if service.GRPC.Host != 0 {
			fmt.Fprintf(
				m.Streams.Out,
				"%s: gRPC 127.0.0.1:%d, admin http://127.0.0.1:%d/healthcheck/live\n",
				service.Name,
				service.GRPC.Host,
				service.Admin.Host,
			)
		}
	}
	p := m.Config.Runtime.Ports
	fmt.Fprintf(
		m.Streams.Out,
		"\nHTTP:    http://localhost:%d/v1/echo\nSwagger: http://localhost:%d/docs/\ngRPC:    127.0.0.1:%d\nAdmin:   http://localhost:%d/healthcheck/live\n",
		p.HTTP,
		p.HTTP,
		p.GRPC,
		p.Admin,
	)
}

func (m *Manager) build(ctx context.Context, state *project.State, services []config.Service) error {
	for _, service := range services {
		fmt.Fprintf(m.Streams.Out, "Building %s\n", service.Name)
		image, err := m.Runtime.Build(ctx, m.Config.Root, service, m.Streams.Out)
		if err != nil {
			return err
		}
		state.Images[service.Name] = image
		if err = m.Store.Save(state); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) deploy(
	ctx context.Context,
	state *project.State,
	services []config.Service,
	client *kube.Client,
) error {
	for _, service := range services {
		if service.Name == "review" || service.Name == "hotels" {
			if err := client.GuardReviewCanary(ctx); err != nil {
				return err
			}
			break
		}
	}
	// Validate/render all selected manifests before mutating Kubernetes resources.
	for _, service := range services {
		image := state.Images[service.Name]
		if image == "" {
			return fmt.Errorf("%s has no built image; run hotelctl build %s", service.Name, service.Name)
		}
		if _, err := kube.RenderService(m.Config, service, image); err != nil {
			return err
		}
	}
	observability, err := kube.RenderObservability(m.Config)
	if err != nil {
		return err
	}
	envoy, err := kube.RenderEnvoy(m.Config)
	if err != nil {
		return err
	}
	if m.Config.Observability.Enabled {
		if err := client.CheckObservabilityPorts(ctx); err != nil {
			return err
		}
	}
	if kube.NeedsBookingDependencies(services) {
		resources, err := kube.RenderBookingDependencies(m.Config.Root)
		if err != nil {
			return err
		}
		fmt.Fprintln(m.Streams.Out, "Запуск PostgreSQL, Kafka и отдельного config-etcd")
		if err = client.Apply(ctx, resources.Infrastructure, m.Streams.Out); err != nil {
			return err
		}
		for _, name := range []string{
			"booking-postgres",
			"check-in-postgres",
			"hotels-postgres",
			"review-postgres",
			"booking-kafka",
			"config-etcd",
			"akhq",
		} {
			if err = client.WaitDeployment(ctx, config.Service{
				Name:       name,
				Deployment: name,
				Namespace:  "hotelflow",
			}); err != nil {
				return err
			}
		}
		fmt.Fprintln(m.Streams.Out, "Применение миграций и учебных данных")
		if err = client.Apply(ctx, resources.Migrations, m.Streams.Out); err != nil {
			return err
		}
		if err = client.WaitJob(ctx, "hotelflow", resources.JobName); err != nil {
			return err
		}
	}
	fmt.Fprintln(m.Streams.Out, "Запуск Envoy для межсервисных RPC")
	if err := client.Apply(ctx, envoy, m.Streams.Out); err != nil {
		return err
	}
	if err := client.WaitDeployment(ctx, kube.EnvoyService()); err != nil {
		return err
	}
	if m.Config.Observability.Enabled {
		fmt.Fprintln(m.Streams.Out, "Запуск инфраструктуры наблюдаемости")
		if err := client.Apply(ctx, observability, m.Streams.Out); err != nil {
			return err
		}
		if err := client.EnsureGrafanaCredentials(ctx); err != nil {
			return err
		}
		for _, component := range m.Config.ObservabilityComponents() {
			if err := client.WaitDeployment(ctx, component); err != nil {
				return err
			}
		}
	}
	for _, service := range services {
		image := state.Images[service.Name]
		fmt.Fprintf(m.Streams.Out, "Deploying %s (%s)\n", service.Name, image)
		if err := m.Runtime.Import(ctx, state, image, m.Config.StateDir(), m.Streams.Out); err != nil {
			return err
		}
		objects, err := kube.RenderService(m.Config, service, image)
		if err != nil {
			return err
		}
		if err = client.Apply(ctx, objects, m.Streams.Out); err != nil {
			return err
		}
		if err = client.WaitDeployment(ctx, service); err != nil {
			return err
		}
	}
	return nil
}
