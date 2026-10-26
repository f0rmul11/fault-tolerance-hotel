package docker

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"slices"
	"strconv"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/errdefs"
	"github.com/docker/go-connections/nat"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/project"
)

const ownerLabel = "io.hotelflow.owner"

const managedLabel = "io.hotelflow.managed-by"

func ContainerName(s *project.State) string {
	return s.Name + "-k3s"
}

func volumeName(s *project.State) string {
	return s.Name + "-k3s-data"
}

func networkName(s *project.State) string {
	return s.Name + "-network"
}

func labels(s *project.State) map[string]string {
	return map[string]string{
		ownerLabel:   s.Owner,
		managedLabel: "hotelctl",
	}
}

func checkOwner(kind, name string, got map[string]string, s *project.State) error {
	if got[ownerLabel] != s.Owner || got[managedLabel] != "hotelctl" {
		return fmt.Errorf("%s %s belongs to another project; refusing to change it", kind, name)
	}
	return nil
}

func (e *Engine) Status(ctx context.Context, s *project.State) (project.RuntimeStatus, error) {
	result := project.RuntimeStatus{
		Name: ContainerName(s),
	}
	info, err := e.client.ContainerInspect(ctx, result.Name)
	if errdefs.IsNotFound(err) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if err := checkOwner("container", result.Name, info.Config.Labels, s); err != nil {
		return result, err
	}
	result.Exists, result.Running = true, info.State.Running
	return result, nil
}

func (e *Engine) Ensure(ctx context.Context, s *project.State, out io.Writer) error {
	info, err := e.client.ContainerInspect(ctx, ContainerName(s))
	if err == nil {
		if err := checkOwner("container", ContainerName(s), info.Config.Labels, s); err != nil {
			return err
		}
		if info.Config.Image != s.Runtime.Image {
			return fmt.Errorf("existing k3s image differs from the configured version; recreate the stand explicitly")
		}
		if reflect.DeepEqual(info.HostConfig.PortBindings, publishedPorts(s)) {
			if info.State.Running {
				return nil
			}
			return e.client.ContainerStart(ctx, info.ID, container.StartOptions{})
		}
		// Port mappings cannot change in place. Keep the persistent k3s volume
		// and owner ID; a subsequent up can recover even if recreation fails.
		vol, err := e.client.VolumeInspect(ctx, volumeName(s))
		if err != nil {
			return err
		}
		if err := checkOwner("volume", volumeName(s), vol.Labels, s); err != nil {
			return err
		}
		networkInfo, err := e.client.NetworkInspect(ctx, networkName(s), network.InspectOptions{})
		if err != nil {
			return err
		}
		if err := checkOwner("network", networkName(s), networkInfo.Labels, s); err != nil {
			return err
		}
		if err := checkNewPortBindings(info.HostConfig.PortBindings, publishedPorts(s)); err != nil {
			return err
		}
		fmt.Fprintln(out, "Updating published ports; restarting k3s with the existing data volume")
		if err := e.Stop(ctx, s); err != nil {
			return err
		}
		if err := e.client.ContainerRemove(ctx, info.ID, container.RemoveOptions{}); err != nil {
			return err
		}
	}
	if err != nil && !errdefs.IsNotFound(err) {
		return err
	}
	if _, err := e.client.ImageInspect(ctx, s.Runtime.Image); err != nil {
		if !errdefs.IsNotFound(err) {
			return err
		}
		fmt.Fprintf(out, "Downloading %s\n", s.Runtime.Image)
		stream, err := e.client.ImagePull(ctx, s.Runtime.Image, image.PullOptions{})
		if err != nil {
			return err
		}
		err = consumeProgress(stream, out)
		stream.Close()
		if err != nil {
			return err
		}
	}
	vol, err := e.client.VolumeInspect(ctx, volumeName(s))
	if errdefs.IsNotFound(err) {
		_, err = e.client.VolumeCreate(ctx, volume.CreateOptions{
			Name:   volumeName(s),
			Labels: labels(s),
		})
	} else if err == nil {
		err = checkOwner("volume", volumeName(s), vol.Labels, s)
	}
	if err != nil {
		return err
	}
	net, err := e.client.NetworkInspect(ctx, networkName(s), network.InspectOptions{})
	if errdefs.IsNotFound(err) {
		_, err = e.client.NetworkCreate(ctx, networkName(s), network.CreateOptions{
			Labels: labels(s),
		})
	} else if err == nil {
		err = checkOwner("network", networkName(s), net.Labels, s)
	}
	if err != nil {
		return err
	}

	ports := publishedPorts(s)
	exposed := nat.PortSet{}
	for p := range ports {
		exposed[p] = struct{}{}
	}
	created, err := e.client.ContainerCreate(ctx, &container.Config{
		Image:        s.Runtime.Image,
		Hostname:     ContainerName(s),
		Labels:       labels(s),
		ExposedPorts: exposed,
		Cmd: []string{
			"server",
			"--node-name",
			ContainerName(s),
			"--disable=traefik,servicelb",
			"--tls-san=127.0.0.1",
			"--tls-san=localhost",
			"--write-kubeconfig-mode=600",
		},
	}, &container.HostConfig{
		Privileged:   true,
		CgroupnsMode: container.CgroupnsModeHost,
		PortBindings: ports,
		NetworkMode:  container.NetworkMode(networkName(s)),
		RestartPolicy: container.RestartPolicy{
			Name: container.RestartPolicyUnlessStopped,
		},
		Tmpfs: map[string]string{
			"/run":     "",
			"/var/run": "",
		},
		Mounts: []mount.Mount{{
			Type:   mount.TypeVolume,
			Source: volumeName(s),
			Target: "/var/lib/rancher/k3s",
		}},
	}, nil, nil, ContainerName(s))
	if err != nil {
		return err
	}
	if err := e.client.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("start k3s; check that ports %v are free: %w", s.Runtime.Ports.All(), err)
	}
	return nil
}

func (e *Engine) Kubeconfig(ctx context.Context, s *project.State) ([]byte, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		reader, _, err := e.client.CopyFromContainer(ctx, ContainerName(s), "/etc/rancher/k3s/k3s.yaml")
		if err == nil {
			tr := tar.NewReader(reader)
			_, err = tr.Next()
			var data []byte
			if err == nil {
				data, err = io.ReadAll(io.LimitReader(tr, 1<<20))
			}
			reader.Close()
			if err == nil && len(data) > 0 {
				return data, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for k3s kubeconfig: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (e *Engine) Stop(ctx context.Context, s *project.State) error {
	status, err := e.Status(ctx, s)
	if err != nil || !status.Exists || !status.Running {
		return err
	}
	timeout := 30
	return e.client.ContainerStop(ctx, ContainerName(s), container.StopOptions{
		Timeout: &timeout,
	})
}

func (e *Engine) Destroy(ctx context.Context, s *project.State) error {
	// Validate every ownership label before removing the first resource.
	status, err := e.Status(ctx, s)
	if err != nil {
		return err
	}
	vol, ve := e.client.VolumeInspect(ctx, volumeName(s))
	if ve != nil && !errdefs.IsNotFound(ve) {
		return ve
	}
	if ve == nil {
		if err := checkOwner("volume", volumeName(s), vol.Labels, s); err != nil {
			return err
		}
	}
	net, ne := e.client.NetworkInspect(ctx, networkName(s), network.InspectOptions{})
	if ne != nil && !errdefs.IsNotFound(ne) {
		return ne
	}
	if ne == nil {
		if err := checkOwner("network", networkName(s), net.Labels, s); err != nil {
			return err
		}
	}
	if status.Exists {
		if err := e.Stop(ctx, s); err != nil {
			return err
		}
		if err := e.client.ContainerRemove(ctx, ContainerName(s), container.RemoveOptions{}); err != nil {
			return err
		}
	}
	var errs []error
	if ve == nil {
		errs = append(errs, e.client.VolumeRemove(ctx, volumeName(s), false))
	}
	if ne == nil {
		errs = append(errs, e.client.NetworkRemove(ctx, networkName(s)))
	}
	return errors.Join(errs...)
}

func (e *Engine) Logs(ctx context.Context, s *project.State, out io.Writer) error {
	reader, err := e.client.ContainerLogs(
		ctx,
		ContainerName(s),
		container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Tail:       "50",
		},
	)
	if err != nil {
		return err
	}
	defer reader.Close()
	return demultiplex(ctx, reader, out, out)
}

func publishedPorts(s *project.State) nat.PortMap {
	ports := nat.PortMap{}
	for _, mapping := range s.PortBindings() {
		p := nat.Port(strconv.Itoa(mapping.Node) + "/tcp")
		bind := s.Runtime.BindAddress
		if mapping.Node == 6443 || mapping.LocalOnly {
			bind = "127.0.0.1"
		}
		ports[p] = []nat.PortBinding{{
			HostIP:   bind,
			HostPort: strconv.Itoa(mapping.Host),
		}}
	}
	return ports
}

// checkNewPortBindings catches host conflicts before the existing node is stopped.
// Existing Docker bindings remain occupied by our own running container.
func checkNewPortBindings(current, desired nat.PortMap) error {
	var existing []nat.PortBinding
	for _, bindings := range current {
		existing = append(existing, bindings...)
	}
	var listeners []net.Listener
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	for _, bindings := range desired {
		for _, binding := range bindings {
			if slices.Contains(existing, binding) {
				continue
			}
			listener, err := net.Listen("tcp", net.JoinHostPort(binding.HostIP, binding.HostPort))
			if err != nil {
				return fmt.Errorf("новый порт %s:%s занят; k3s оставлен без изменений: %w", binding.HostIP, binding.HostPort, err)
			}
			listeners = append(listeners, listener)
		}
	}
	return nil
}
