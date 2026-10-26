package kube

import (
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

func TestMetricsStagePreservesBackendsAndUsesOTLPOnly(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	c.Observability.Stage = "logs"
	previous, err := RenderObservability(c)
	if err != nil {
		t.Fatal(err)
	}
	templates := make(map[string]map[string]any)
	for _, object := range previous {
		if object.GetKind() == "Deployment" && (object.GetName() == "jaeger" || object.GetName() == "loki") {
			templates[object.GetName()], _, _ = unstructured.NestedMap(object.Object, "spec", "template")
		}
	}
	ports := c.PortBindings()
	c.Observability.Stage = "metrics"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	objects, err := RenderObservability(c)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ports, c.PortBindings()) {
		t.Fatal("metrics must not recreate k3s to change host ports")
	}
	if len(c.ObservabilityComponents()) != 5 {
		t.Fatal("metrics stage must contain five backends")
	}
	deployments, pvcs := 0, 0
	for _, object := range objects {
		switch object.GetKind() {
		case "Deployment":
			deployments++
			if before, ok := templates[object.GetName()]; ok {
				after, _, _ := unstructured.NestedMap(object.Object, "spec", "template")
				if !reflect.DeepEqual(before, after) {
					t.Fatalf("metrics would restart %s", object.GetName())
				}
			}
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
						t.Fatal("Grafana must retain Loki and add Prometheus")
					}
				}
			}
		}
	}
	if deployments != 5 || pvcs != 3 {
		t.Fatalf("deployments=%d PVCs=%d", deployments, pvcs)
	}
	for _, stage := range []string{"traces", "logs", "metrics"} {
		c.Observability.Stage = stage
		for _, enabled := range []bool{false, true} {
			c.Observability.Enabled = enabled
			objects, err := RenderService(c, c.Services[0], "hotelflow/test:metrics")
			if err != nil {
				t.Fatal(err)
			}
			for _, object := range objects {
				if object.GetKind() != "Deployment" {
					continue
				}
				var deployment appsv1.Deployment
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &deployment); err != nil {
					t.Fatal(err)
				}
				values := make(map[string]string)
				for _, env := range deployment.Spec.Template.Spec.Containers[0].Env {
					values[env.Name] = env.Value
				}
				if values["TELEMETRY_METRICS_ENABLED"] != strconv.FormatBool(enabled && stage == "metrics") || values["TELEMETRY_LOGS_ENABLED"] != strconv.FormatBool(enabled && stage != "traces") {
					t.Fatalf("wrong telemetry switches for %s enabled=%t", stage, enabled)
				}
			}
		}
	}
}
