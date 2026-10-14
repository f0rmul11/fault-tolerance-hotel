package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	dockerconfig "github.com/docker/cli/cli/config"
	dockercontext "github.com/docker/cli/cli/context/docker"
	"github.com/docker/cli/cli/context/store"
	"github.com/docker/docker/client"
)

type Engine struct {
	client client.APIClient
}

// New respects the active Docker context, including Docker Desktop and OrbStack.
// All operations use the Engine SDK, never a shell or the docker executable.
func New(errOut io.Writer) (*Engine, error) {
	cfg := dockerconfig.LoadDefaultConfigFile(errOut)
	name := os.Getenv("DOCKER_CONTEXT")
	if name == "" && os.Getenv("DOCKER_HOST") == "" {
		name = cfg.CurrentContext
	}
	opts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}
	if name != "" && name != "default" {
		contextStore := store.New(dockerconfig.ContextStoreDir(), store.NewConfig(
			func() any {
				return &map[string]any{}
			},
			store.EndpointTypeGetter(dockercontext.DockerEndpoint, func() any {
				return &dockercontext.EndpointMeta{}
			}),
		))
		metadata, err := contextStore.GetMetadata(name)
		if err != nil {
			return nil, fmt.Errorf("Docker context %s: %w", name, err)
		}
		endpoint, err := dockercontext.EndpointFromContext(metadata)
		if err != nil {
			return nil, err
		}
		// A local stand must bind ports on this computer, never on a remote daemon.
		if !strings.HasPrefix(endpoint.Host, "unix://") && !strings.HasPrefix(endpoint.Host, "npipe://") {
			return nil, fmt.Errorf("Docker context %s is remote; select a local context", name)
		}
		opts = []client.Opt{
			client.WithHost(endpoint.Host),
			client.WithAPIVersionNegotiation(),
		}
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}
	return &Engine{
		client: cli,
	}, nil
}

func (e *Engine) Close() error {
	return e.client.Close()
}

func (e *Engine) Check(ctx context.Context) error {
	host := e.client.DaemonHost()
	if !strings.HasPrefix(host, "unix://") && !strings.HasPrefix(host, "npipe://") {
		return fmt.Errorf("a local Docker context is required; current endpoint is %s", host)
	}
	info, err := e.client.Info(ctx)
	if err != nil {
		return fmt.Errorf("Docker is unavailable; start Docker Desktop, Docker Engine or OrbStack: %w", err)
	}
	if info.OSType != "linux" {
		return fmt.Errorf("Docker must run Linux containers (current OS: %s)", info.OSType)
	}
	for _, option := range info.SecurityOptions {
		if strings.Contains(option, "rootless") {
			return fmt.Errorf("k3s needs privileged containers; rootless Docker is not supported")
		}
	}
	return nil
}
