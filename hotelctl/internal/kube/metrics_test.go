package kube

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func TestObservabilityUsesOTLPForAllSignals(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	objects, err := RenderObservability(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ObservabilityComponents()) != 5 {
		t.Fatal("observability must contain five backends")
	}
	deployments, pvcs := 0, 0
	for _, object := range objects {
		switch object.GetKind() {
		case "Deployment":
			deployments++
		case "PersistentVolumeClaim":
			pvcs++
		case "ConfigMap":
			data, _, _ := unstructured.NestedStringMap(object.Object, "data")
			for name, content := range data {
				var parsed map[string]any
				if !strings.HasSuffix(name, ".yaml") {
					continue
				}
				if err := yaml.UnmarshalStrict([]byte(content), &parsed); err != nil {
					t.Fatalf("%s/%s: %v", object.GetName(), name, err)
				}
				if strings.HasPrefix(object.GetName(), "otel-collector-config-") {
					pipelines := parsed["service"].(map[string]any)["pipelines"].(map[string]any)
					if len(pipelines) != 3 || pipelines["metrics"] == nil || pipelines["traces"] == nil || pipelines["logs"] == nil {
						t.Fatal("collector lost one of its pipelines")
					}
					receivers := parsed["receivers"].(map[string]any)
					if len(receivers) != 1 || receivers["otlp"] == nil {
						t.Fatal("collector must not scrape applications or infrastructure")
					}
				}
				if strings.HasPrefix(object.GetName(), "prometheus-config-") && len(parsed["scrape_configs"].([]any)) != 0 {
					t.Fatal("Prometheus must receive pushed OTLP only")
				}
				if strings.HasPrefix(object.GetName(), "grafana-datasources-") {
					sources := parsed["datasources"].([]any)
					if len(sources) != 2 {
						t.Fatal("Grafana must contain Loki and Prometheus")
					}
				}
			}
		}
	}
	if deployments != 5 || pvcs != 3 {
		t.Fatalf("deployments=%d PVCs=%d", deployments, pvcs)
	}
}
