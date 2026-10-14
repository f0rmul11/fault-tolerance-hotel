package stand

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"text/tabwriter"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/diagnostics"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/kube"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/project"
)

func (m *Manager) Smoke(ctx context.Context) error {
	return diagnostics.Smoke(ctx, m.Config, m.Config.Services, m.Streams.Out)
}

func (m *Manager) Status(ctx context.Context, format string) error {
	result := struct {
		Runtime  project.RuntimeStatus `json:"runtime"`
		Services []kube.ServiceStatus  `json:"services"`
		Error    string                `json:"error,omitempty"`
	}{}
	state, err := m.Store.Load()
	if err != nil {
		return err
	}
	if state != nil {
		result.Runtime, err = m.Runtime.Status(ctx, state)
		if err != nil {
			return err
		}
		if result.Runtime.Running {
			client, e := kube.New(m.Config.Kubeconfig())
			if e == nil {
				result.Services, e = client.Services(ctx, m.Config.Services)
			}
			if e != nil {
				result.Error = e.Error()
			}
		}
	}
	if format == "json" {
		enc := json.NewEncoder(m.Streams.Out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return err
		}
		if result.Error != "" {
			return fmt.Errorf("Kubernetes: %s", result.Error)
		}
		return nil
	}
	if !result.Runtime.Exists {
		fmt.Fprintln(m.Streams.Out, "Stand is not created. Run hotelctl up.")
		return nil
	}
	if !result.Runtime.Running {
		fmt.Fprintln(m.Streams.Out, "Stand is stopped. Run hotelctl up.")
		return nil
	}
	fmt.Fprintf(m.Streams.Out, "k3s node: %s (running)\n", result.Runtime.Name)
	w := tabwriter.NewWriter(m.Streams.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SERVICE\tREADY\tgRPC\tADMIN\tIMAGE")
	for _, s := range result.Services {
		grpcPort, adminPort := s.GRPCPort, s.AdminPort
		if s.Name == "api-gateway" {
			grpcPort, adminPort = m.Config.Runtime.Ports.GRPC, m.Config.Runtime.Ports.Admin
		}
		fmt.Fprintf(
			w,
			"%s\t%d/%d\t127.0.0.1:%d\t127.0.0.1:%d\t%s\n",
			s.Name,
			s.Ready,
			s.Desired,
			grpcPort,
			adminPort,
			s.Image,
		)
	}
	_ = w.Flush()
	if result.Error != "" {
		return fmt.Errorf("Kubernetes: %s", result.Error)
	}
	return nil
}

func (m *Manager) Doctor(ctx context.Context) error {
	fmt.Fprintln(m.Streams.Out, "OK project configuration")
	if err := m.Runtime.Check(ctx); err != nil {
		return err
	}
	fmt.Fprintln(m.Streams.Out, "OK local Docker with Linux containers")
	state, err := m.Store.Load()
	if err != nil {
		return err
	}
	if state != nil {
		if err = m.compatible(state); err != nil {
			return err
		}
		status, err := m.Runtime.Status(ctx, state)
		if err != nil {
			return err
		}
		if status.Running {
			client, err := kube.New(m.Config.Kubeconfig())
			if err != nil {
				return err
			}
			if err = client.WaitNode(ctx); err != nil {
				return err
			}
			fmt.Fprintln(m.Streams.Out, "OK one ready k3s node")
			return nil
		}
	}
	for _, binding := range m.Config.PortBindings() {
		port := binding.Host
		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			return fmt.Errorf("port %d is occupied; stop the process using it or change hotel.yaml: %w", port, err)
		}
		listener.Close()
	}
	fmt.Fprintln(m.Streams.Out, "OK host ports are available. Run hotelctl up.")
	return nil
}

// BusinessSmoke проверяет публичный сценарий и результат доставки в stdout Notification.
func (m *Manager) BusinessSmoke(ctx context.Context) error {
	if _, err := m.running(ctx); err != nil {
		return err
	}
	client, err := kube.New(m.Config.Kubeconfig())
	if err != nil {
		return err
	}
	return diagnostics.BusinessSmoke(ctx, m.Config, client, m.Streams.Out)
}
