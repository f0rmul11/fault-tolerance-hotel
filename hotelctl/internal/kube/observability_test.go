package kube

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestObservabilityRenderHasOneCollectorAndAllBackends(t *testing.T) {
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
	deployments := map[string]bool{}
	configMaps := map[string]bool{}
	pvcs := map[string]bool{}
	uiPorts := map[string]int32{}
	collectorConfigs := 0
	for _, object := range objects {
		switch object.GetKind() {
		case "ConfigMap":
			configMaps[object.GetName()] = true
			if strings.HasPrefix(object.GetName(), "otel-collector-config-") {
				collectorConfigs++
			}
		case "Deployment":
			deployments[object.GetName()] = true
			var deployment appsv1.Deployment
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &deployment); err != nil {
				t.Fatal(err)
			}
			if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 || deployment.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
				t.Fatal("observability infrastructure must remain single-instance during rollout")
			}
		case "PersistentVolumeClaim":
			pvcs[object.GetName()] = true
		case "Service":
			var service corev1.Service
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &service); err != nil {
				t.Fatal(err)
			}
			if service.Spec.Type == corev1.ServiceTypeNodePort {
				uiPorts[service.Name] = service.Spec.Ports[0].NodePort
			}
		}
	}
	if len(deployments) != 5 || collectorConfigs != 1 {
		t.Fatalf("wrong infrastructure: deployments=%v collector configs=%d", deployments, collectorConfigs)
	}
	for _, name := range []string{"otel-collector", "jaeger", "loki", "grafana", "prometheus"} {
		if !deployments[name] {
			t.Fatalf("missing deployment %s", name)
		}
	}
	if len(pvcs) != 3 || !pvcs["loki-data"] || !pvcs["grafana-data"] || !pvcs["prometheus-data"] {
		t.Fatalf("unexpected PVCs (Jaeger must use memory): %v", pvcs)
	}
	if len(uiPorts) != 2 || uiPorts["grafana"] != 30300 || uiPorts["jaeger-ui"] != 30686 {
		t.Fatalf("unexpected UI ports: %v", uiPorts)
	}
	for _, object := range objects {
		if object.GetKind() != "Deployment" {
			continue
		}
		var deployment appsv1.Deployment
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &deployment); err != nil {
			t.Fatal(err)
		}
		for _, volume := range deployment.Spec.Template.Spec.Volumes {
			if volume.ConfigMap != nil && !configMaps[volume.ConfigMap.Name] {
				t.Fatalf("unresolved generated configMap: %s", volume.ConfigMap.Name)
			}
		}
	}
	c.Observability.Enabled = false
	objects, err = RenderObservability(c)
	if err != nil || len(objects) != 0 {
		t.Fatalf("disabled observability rendered resources: %v, %v", objects, err)
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
					"TELEMETRY_METRICS_ENABLED":   strconv.FormatBool(enabled),
					"TELEMETRY_LOGS_ENABLED":      strconv.FormatBool(enabled),
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
