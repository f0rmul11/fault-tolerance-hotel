package stand

import (
	"context"
	"fmt"
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
	state.Bindings = m.Config.PortBindings()
	if err = m.Store.Save(state); err != nil {
		return err
	}
	fmt.Fprintln(m.Streams.Out, "Starting one k3s node")
	if err = m.Runtime.Ensure(ctx, state, m.Streams.Out); err != nil {
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
	// Validate/render all selected manifests before mutating Kubernetes resources.
	for _, service := range services {
		image := state.Images[service.Name]
		if image == "" {
			return fmt.Errorf("%s has no built image; run hotelctl build %s", service.Name, service.Name)
		}
		if _, err := kube.Render(m.Config.Root, service, image); err != nil {
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
	for _, service := range services {
		image := state.Images[service.Name]
		fmt.Fprintf(m.Streams.Out, "Deploying %s (%s)\n", service.Name, image)
		if err := m.Runtime.Import(ctx, state, image, m.Config.StateDir(), m.Streams.Out); err != nil {
			return err
		}
		objects, err := kube.Render(m.Config.Root, service, image)
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
