package kube

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/fault"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// EnvoyService is a stock Envoy infrastructure component.
func EnvoyService() config.Service {
	return config.Service{
		Name:       "envoy",
		Deployment: "envoy",
		Namespace:  "hotelflow",
		Manifests:  "deploy/envoy",
	}
}

func RenderEnvoy(c config.Config) ([]*unstructured.Unstructured, error) {
	content, err := os.ReadFile(filepath.Join(c.Root, "deploy/envoy/envoy.yaml.tmpl"))
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("envoy").Option("missingkey=error").Parse(string(content))
	if err != nil {
		return nil, err
	}
	type upstream struct {
		Name    string
		Address string
	}
	var upstreams []upstream
	seen := map[string]bool{}
	for _, edge := range fault.Edges() {
		if seen[edge.To] {
			continue
		}
		seen[edge.To] = true
		namespace := "hotelflow"
		for _, service := range c.Services {
			if service.Name == edge.To {
				namespace = service.Namespace
			}
		}
		upstreams = append(upstreams, upstream{
			Name:    edge.To,
			Address: edge.To + "." + namespace + ".svc.cluster.local",
		})
	}
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, struct {
		Edges     []fault.Edge
		Upstreams []upstream
	}{
		Edges:     fault.Edges(),
		Upstreams: upstreams,
	}); err != nil {
		return nil, err
	}
	bootstrap, err := yaml.YAMLToJSON(rendered.Bytes())
	if err != nil {
		return nil, fmt.Errorf("конфигурация Envoy: %w", err)
	}
	objects, err := Render(c.Root, EnvoyService(), "")
	if err != nil {
		return nil, err
	}
	checksum := sha256.Sum256(bootstrap)
	for _, object := range objects {
		if object.GetKind() == "Deployment" {
			if err := unstructured.SetNestedField(object.Object, fmt.Sprintf("%x", checksum), "spec", "template", "metadata", "annotations", "hotelflow/config-sha256"); err != nil {
				return nil, err
			}
		}
	}
	cm := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "envoy-config",
			"namespace": "hotelflow",
		},
		"data": map[string]any{
			"envoy.json": string(bootstrap),
		},
	}}
	// Namespace must precede ConfigMap on a fresh cluster; ConfigMap precedes Pod.
	return append(objects[:1], append([]*unstructured.Unstructured{cm}, objects[1:]...)...), nil
}

func configureEnvoyTargets(object *unstructured.Unstructured, service config.Service) error {
	if object.GetKind() != "Deployment" || object.GetName() != service.Deployment {
		return nil
	}
	var targets []string
	for _, edge := range fault.Edges() {
		if edge.From == service.Name {
			targets = append(targets, fmt.Sprintf("%s:envoy.hotelflow.svc.cluster.local:%d", edge.To, edge.Port))
		}
	}
	if len(targets) == 0 {
		return nil
	}
	containers, _, err := unstructured.NestedSlice(object.Object, "spec", "template", "spec", "containers")
	if err != nil {
		return err
	}
	for _, raw := range containers {
		container, ok := raw.(map[string]any)
		if !ok || container["name"] != service.Name {
			continue
		}
		env, _, err := unstructured.NestedSlice(container, "env")
		if err != nil {
			return err
		}
		found := false
		for _, raw := range env {
			entry, ok := raw.(map[string]any)
			if ok && entry["name"] == "SERVICE_TARGETS" {
				entry["value"] = strings.Join(targets, ",")
				delete(entry, "valueFrom")
				found = true
			}
		}
		if !found {
			env = append(env, map[string]any{
				"name":  "SERVICE_TARGETS",
				"value": strings.Join(targets, ","),
			})
		}
		container["env"] = env
	}
	return unstructured.SetNestedSlice(object.Object, containers, "spec", "template", "spec", "containers")
}
