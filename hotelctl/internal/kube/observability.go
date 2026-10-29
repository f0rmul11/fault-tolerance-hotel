package kube

import (
	"context"
	"fmt"
	"strconv"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// RenderObservability renders the complete observability stack.
func RenderObservability(c config.Config) ([]*unstructured.Unstructured, error) {
	if !c.Observability.Enabled {
		return nil, nil
	}
	return Render(c.Root, config.Service{
		Name:      "observability",
		Manifests: "deploy/observability",
	}, "")
}

// RenderService passes the stand telemetry switch into each application config.
func RenderService(c config.Config, service config.Service, image string) ([]*unstructured.Unstructured, error) {
	objects, err := Render(c.Root, service, image)
	if err != nil {
		return nil, err
	}
	for _, object := range objects {
		if err := configureEnvoyTargets(object, service); err != nil {
			return nil, err
		}
	}
	settings := []struct {
		name  string
		value string
	}{
		{name: "TELEMETRY_ENABLED", value: strconv.FormatBool(c.Observability.Enabled)},
		{name: "TELEMETRY_METRICS_ENABLED", value: strconv.FormatBool(c.Observability.Enabled)},
		{name: "TELEMETRY_LOGS_ENABLED", value: strconv.FormatBool(c.Observability.Enabled)},
	}
	for _, object := range objects {
		if (object.GetKind() != "Deployment" && object.GetKind() != "Rollout") || object.GetName() != service.Deployment {
			continue
		}
		containers, _, err := unstructured.NestedSlice(object.Object, "spec", "template", "spec", "containers")
		if err != nil {
			return nil, err
		}
		for _, item := range containers {
			container, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s: некорректный container", service.Name)
			}
			env, _, err := unstructured.NestedSlice(container, "env")
			if err != nil {
				return nil, err
			}
			for _, setting := range settings {
				found := false
				for _, raw := range env {
					entry, ok := raw.(map[string]any)
					if ok && entry["name"] == setting.name {
						entry["value"] = setting.value
						delete(entry, "valueFrom")
						found = true
					}
				}
				if !found {
					env = append(env, map[string]any{
						"name":  setting.name,
						"value": setting.value,
					})
				}
			}
			if err := unstructured.SetNestedSlice(container, env, "env"); err != nil {
				return nil, err
			}
		}
		if err := unstructured.SetNestedSlice(object.Object, containers, "spec", "template", "spec", "containers"); err != nil {
			return nil, err
		}
	}
	return objects, nil
}

// CheckObservabilityPorts refuses collisions before runtime restart or resource apply.
func (c *Client) CheckObservabilityPorts(ctx context.Context) error {
	services, err := c.Typed.CoreV1().Services(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("проверка NodePort наблюдаемости: %w", err)
	}
	owners := map[int32]string{
		30686: "jaeger-ui",
		30300: "grafana",
	}
	for _, service := range services.Items {
		for _, port := range service.Spec.Ports {
			owner, reserved := owners[port.NodePort]
			if reserved && (service.Namespace != "hotelflow" || service.Name != owner) {
				return fmt.Errorf("NodePort %d занят Service %s/%s; k3s оставлен без изменений", port.NodePort, service.Namespace, service.Name)
			}
		}
	}
	return nil
}
