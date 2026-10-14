package kube

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/project"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Client struct {
	Typed   kubernetes.Interface
	Dynamic dynamic.Interface
	REST    *rest.Config
}

func WriteConfig(data []byte, path string, port int) error {
	cfg, err := clientcmd.Load(data)
	if err != nil {
		return err
	}
	if len(cfg.Clusters) != 1 {
		return fmt.Errorf("expected a single cluster in k3s kubeconfig")
	}
	for _, cluster := range cfg.Clusters {
		cluster.Server = "https://127.0.0.1:" + strconv.Itoa(port)
	}
	raw, err := clientcmd.Write(*cfg)
	if err != nil {
		return err
	}
	return project.AtomicWrite(path, raw)
}

func New(path string) (*Client, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		return nil, fmt.Errorf("read course kubeconfig; run hotelctl up: %w", err)
	}
	cfg.Timeout = 10 * time.Second
	cfg.UserAgent = "hotelctl"
	typed, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{
		Typed:   typed,
		Dynamic: dyn,
		REST:    cfg,
	}, nil
}

func Poll(ctx context.Context, description string, check func(context.Context) (bool, error)) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var last error
	for {
		ok, err := check(ctx)
		if ok && err == nil {
			return nil
		}
		last = err
		select {
		case <-ctx.Done():
			if last != nil {
				return fmt.Errorf("%s: %w (%v)", description, ctx.Err(), last)
			}
			return fmt.Errorf("%s: %w", description, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (c *Client) WaitNode(ctx context.Context) error {
	return Poll(ctx, "waiting for the k3s node", func(ctx context.Context) (bool, error) {
		nodes, err := c.Typed.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		if len(nodes.Items) != 1 {
			return false, fmt.Errorf("expected one node, got %d", len(nodes.Items))
		}
		for _, condition := range nodes.Items[0].Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
}

func (c *Client) WaitDeployment(ctx context.Context, s config.Service) error {
	return Poll(ctx, "waiting for "+s.Name, func(ctx context.Context) (bool, error) {
		d, err := c.Typed.AppsV1().Deployments(s.Namespace).Get(ctx, s.Deployment, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		for _, condition := range d.Status.Conditions {
			if condition.Reason == "ProgressDeadlineExceeded" {
				return false, fmt.Errorf("deployment exceeded its progress deadline: %s", condition.Message)
			}
		}
		return d.Status.ObservedGeneration >= d.Generation &&
			d.Status.UpdatedReplicas == desired &&
			d.Status.AvailableReplicas == desired &&
			d.Status.Replicas == desired, nil
	})
}
