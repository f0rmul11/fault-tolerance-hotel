package kube

import (
	"context"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"
)

func TestTracingRenderHasOneCollectorAndNoFutureBackends(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	c.Observability.Stage = "traces"
	objects, err := RenderObservability(c)
	if err != nil {
		t.Fatal(err)
	}
	deployments := map[string]bool{}
	configMaps := map[string]bool{}
	var reservedGrafana bool
	for _, object := range objects {
		switch object.GetKind() {
		case "ConfigMap":
			configMaps[object.GetName()] = true
			data, _, err := unstructured.NestedString(object.Object, "data", "config.yaml")
			if err != nil {
				t.Fatal(err)
			}
			var cfg map[string]any
			if err := yaml.UnmarshalStrict([]byte(data), &cfg); err != nil {
				t.Fatal(err)
			}
			service := cfg["service"].(map[string]any)
			pipelines := service["pipelines"].(map[string]any)
			if len(pipelines) != 1 || pipelines["traces"] == nil {
				t.Fatal("tracing config includes a future signal")
			}
		case "Deployment":
			deployments[object.GetName()] = true
			var deployment appsv1.Deployment
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &deployment); err != nil {
				t.Fatal(err)
			}
			if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 || deployment.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
				t.Fatal("tracing infrastructure must remain single-instance during rollout")
			}
		case "PersistentVolumeClaim":
			t.Fatal("Jaeger memory storage must not acquire a PVC")
		case "Service":
			if object.GetName() == "grafana" {
				var service corev1.Service
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &service); err != nil {
					t.Fatal(err)
				}
				reservedGrafana = service.Spec.Ports[0].NodePort == 30300
			}
		}
	}
	if len(deployments) != 2 || !deployments["otel-collector"] || !deployments["jaeger"] || !reservedGrafana {
		t.Fatalf("wrong infrastructure: deployments=%v Grafana port=%v", deployments, reservedGrafana)
	}
	for _, object := range objects {
		if object.GetKind() != "Deployment" {
			continue
		}
		volumes, _, _ := unstructured.NestedSlice(object.Object, "spec", "template", "spec", "volumes")
		for _, raw := range volumes {
			volume := raw.(map[string]any)
			cm := volume["configMap"].(map[string]any)
			if !configMaps[cm["name"].(string)] {
				t.Fatalf("unresolved generated configMap: %v", cm)
			}
		}
	}
}

func TestNodePortPreflightRejectsForeignService(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		service   string
		wantError bool
	}{
		{
			name:      "reservation owned by stand",
			namespace: "hotelflow",
			service:   "grafana",
		},
		{
			name:      "foreign namespace",
			namespace: "other",
			service:   "grafana",
			wantError: true,
		},
		{
			name:      "other service",
			namespace: "hotelflow",
			service:   "unrelated-ui",
			wantError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &Client{
				Typed: fake.NewSimpleClientset(&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      test.service,
						Namespace: test.namespace,
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{NodePort: 30300},
						},
					},
				}),
			}
			if err := client.CheckObservabilityPorts(context.Background()); (err != nil) != test.wantError {
				t.Fatalf("unexpected preflight result: %v", err)
			}
		})
	}
}

func TestRenderServiceConfiguresTelemetryEnablement(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		for _, service := range c.Services {
			t.Run(service.Name+"/enabled="+strconv.FormatBool(enabled), func(t *testing.T) {
				c.Observability.Enabled = enabled
				objects, err := RenderService(c, service, "hotelflow/"+service.Name+":test")
				if err != nil {
					t.Fatal(err)
				}
				var deployment appsv1.Deployment
				for _, object := range objects {
					if object.GetKind() == "Deployment" && object.GetName() == service.Deployment {
						if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &deployment); err != nil {
							t.Fatal(err)
						}
					}
				}
				if len(deployment.Spec.Template.Spec.Containers) != 1 {
					t.Fatal("application deployment must contain one application container")
				}
				env := make(map[string]string)
				for _, entry := range deployment.Spec.Template.Spec.Containers[0].Env {
					env[entry.Name] = entry.Value
				}
				for name, want := range map[string]string{
					"TELEMETRY_ENABLED":           strconv.FormatBool(enabled),
					"TELEMETRY_LOGS_ENABLED":      strconv.FormatBool(enabled && (c.Observability.Stage == "logs" || c.Observability.Stage == "metrics")),
					"OTEL_EXPORTER_OTLP_ENDPOINT": "otel-collector.hotelflow.svc.cluster.local:4317",
					"OTEL_EXPORTER_OTLP_INSECURE": "true",
				} {
					if env[name] != want {
						t.Errorf("%s = %q, want %q", name, env[name], want)
					}
				}
				for _, name := range []string{"OTEL_TRACES_EXPORTER", "OTEL_EXPORTER_OTLP_PROTOCOL", "OTEL_PROPAGATORS"} {
					if _, exists := env[name]; exists {
						t.Errorf("obsolete SDK configuration %s is still present", name)
					}
				}
			})
		}
	}
}

func TestLogsStagePreservesTracingAndUIBindings(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	c.Observability.Stage = "traces"
	before, err := RenderObservability(c)
	if err != nil {
		t.Fatal(err)
	}
	var jaegerBefore map[string]any
	for _, object := range before {
		if object.GetKind() == "Deployment" && object.GetName() == "jaeger" {
			jaegerBefore, _, _ = unstructured.NestedMap(object.Object, "spec", "template")
		}
	}
	portsBefore := c.PortBindings()
	c.Observability.Stage = "logs"
	after, err := RenderObservability(c)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(portsBefore, c.PortBindings()) {
		t.Fatal("logs stage would change runtime port bindings")
	}
	deployments := map[string]bool{}
	pvcs := map[string]bool{}
	for _, object := range after {
		if object.GetKind() == "PersistentVolumeClaim" {
			pvcs[object.GetName()] = true
		}
		if object.GetKind() == "Deployment" {
			deployments[object.GetName()] = true
			if object.GetName() == "jaeger" {
				template, _, _ := unstructured.NestedMap(object.Object, "spec", "template")
				if !reflect.DeepEqual(jaegerBefore, template) {
					t.Fatal("adding logs would restart memory-only Jaeger")
				}
			}
		}
		if object.GetKind() == "ConfigMap" && strings.HasPrefix(object.GetName(), "otel-collector-config-") {
			data, _, _ := unstructured.NestedString(object.Object, "data", "config.yaml")
			var cfg map[string]any
			if err := yaml.UnmarshalStrict([]byte(data), &cfg); err != nil {
				t.Fatal(err)
			}
			pipelines := cfg["service"].(map[string]any)["pipelines"].(map[string]any)
			if len(pipelines) != 2 || pipelines["traces"] == nil || pipelines["logs"] == nil {
				t.Fatal("logs stage must retain traces and exclude metrics")
			}
		}
	}
	if len(deployments) != 4 || !deployments["loki"] || !deployments["grafana"] || len(pvcs) != 2 || !pvcs["loki-data"] || !pvcs["grafana-data"] {
		t.Fatalf("unexpected resources: deployments=%v PVCs=%v", deployments, pvcs)
	}
	for _, stage := range []string{"traces", "logs"} {
		c.Observability.Stage = stage
		objects, err := RenderService(c, c.Services[0], "hotelflow/test:config")
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
			for _, env := range deployment.Spec.Template.Spec.Containers[0].Env {
				if env.Name == "TELEMETRY_LOGS_ENABLED" && env.Value != strconv.FormatBool(stage == "logs") {
					t.Fatalf("wrong logs switch for %s", stage)
				}
			}
		}
	}
}
