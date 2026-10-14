package kube

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ServiceStatus struct {
	Name      string `json:"name"`
	Ready     int32  `json:"ready"`
	Desired   int32  `json:"desired"`
	Image     string `json:"image"`
	GRPCPort  int    `json:"grpcPort,omitempty"`
	AdminPort int    `json:"adminPort,omitempty"`
}

func (c *Client) Services(ctx context.Context, services []config.Service) ([]ServiceStatus, error) {
	var result []ServiceStatus
	for _, s := range services {
		d, err := c.Typed.AppsV1().Deployments(s.Namespace).Get(ctx, s.Deployment, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		item := ServiceStatus{
			Name:      s.Name,
			Ready:     d.Status.ReadyReplicas,
			Desired:   desired,
			GRPCPort:  s.GRPC.Host,
			AdminPort: s.Admin.Host,
		}
		if len(d.Spec.Template.Spec.Containers) > 0 {
			item.Image = d.Spec.Template.Spec.Containers[0].Image
		}
		result = append(result, item)
	}
	return result, nil
}

func (c *Client) Logs(ctx context.Context, s config.Service, follow bool, tail int64, out io.Writer) error {
	d, err := c.Typed.AppsV1().Deployments(s.Namespace).Get(ctx, s.Deployment, metav1.GetOptions{})
	if err != nil {
		return err
	}
	selector, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil {
		return err
	}
	pods, err := c.Typed.CoreV1().Pods(s.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return err
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no pods for %s", s.Name)
	}
	sort.Slice(pods.Items, func(i, j int) bool {
		return pods.Items[j].CreationTimestamp.Before(&pods.Items[i].CreationTimestamp)
	})
	pod := pods.Items[0]
	fmt.Fprintf(out, "Pod: %s\n", pod.Name)
	stream, err := c.Typed.CoreV1().Pods(s.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Follow:    follow,
		TailLines: &tail,
	}).Stream(ctx)
	if err != nil {
		return err
	}
	defer stream.Close()
	_, err = io.Copy(out, stream)
	return err
}

func (c *Client) Diagnostics(ctx context.Context, services []config.Service, out io.Writer) {
	namespaces := map[string]bool{}
	for _, s := range services {
		if namespaces[s.Namespace] {
			continue
		}
		namespaces[s.Namespace] = true
		pods, err := c.Typed.CoreV1().Pods(s.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintln(out, err)
			continue
		}
		for _, pod := range pods.Items {
			fmt.Fprintf(out, "%s/%s: %s\n", s.Namespace, pod.Name, pod.Status.Phase)
			for _, status := range pod.Status.ContainerStatuses {
				if status.State.Waiting != nil {
					fmt.Fprintf(out, "  %s: %s\n", status.State.Waiting.Reason, status.State.Waiting.Message)
				}
			}
		}
		events, err := c.Typed.CoreV1().Events(s.Namespace).List(ctx, metav1.ListOptions{
			FieldSelector: "type=Warning",
		})
		if err == nil {
			for _, event := range events.Items {
				fmt.Fprintf(out, "%s: %s\n", event.Reason, event.Message)
			}
		}
	}
}
