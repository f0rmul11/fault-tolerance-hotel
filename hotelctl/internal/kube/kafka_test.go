package kube

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestKafkaNodesHaveIndependentIdentityAndReachableListeners(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	resources, err := RenderBookingDependencies(root)
	if err != nil {
		t.Fatal(err)
	}
	deployments := map[string]appsv1.Deployment{}
	services := map[string]corev1.Service{}
	for _, resource := range resources.Infrastructure {
		switch resource.GetKind() {
		case "Deployment":
			var deployment appsv1.Deployment
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &deployment); err != nil {
				t.Fatal(err)
			}
			deployments[deployment.Name] = deployment
		case "Service":
			var service corev1.Service
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, &service); err != nil {
				t.Fatal(err)
			}
			services[service.Name] = service
		}
	}
	volumes := map[string]bool{}
	for node, name := range []string{"booking-kafka", "booking-kafka-2", "booking-kafka-3"} {
		deployment := deployments[name]
		if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 || deployment.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
			t.Fatalf("%s: stable node identity requires a single non-overlapping pod", name)
		}
		container := deployment.Spec.Template.Spec.Containers[0]
		env := map[string]string{}
		for _, item := range container.Env {
			env[item.Name] = item.Value
		}
		if env["KAFKA_NODE_ID"] != fmt.Sprint(node+1) || env["KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR"] != "3" || env["KAFKA_MIN_INSYNC_REPLICAS"] != "2" {
			t.Fatalf("%s: invalid identity or durability configuration", name)
		}
		if !strings.Contains(env["KAFKA_ADVERTISED_LISTENERS"], fmt.Sprintf("PRODUCER://kafka-proxy:%d", 19091+node)) {
			t.Fatalf("%s: metadata would bypass the producer fault", name)
		}
		claim := deployment.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName
		if claim == "" || volumes[claim] {
			t.Fatalf("%s: data volume must be private", name)
		}
		volumes[claim] = true
		service := services[fmt.Sprintf("kafka-%d", node+1)]
		if !service.Spec.PublishNotReadyAddresses || service.Spec.Selector["app.kubernetes.io/name"] != name {
			t.Fatalf("%s: quorum must reach this node before readiness", name)
		}
		for _, port := range service.Spec.Ports {
			if port.TargetPort.Type != intstr.String {
				continue
			}
			found := false
			for _, declared := range container.Ports {
				found = found || declared.Name == port.TargetPort.StrVal
			}
			if !found {
				t.Fatalf("%s: service target %s is not declared", name, port.TargetPort.StrVal)
			}
		}
	}
	if services["kafka"].Spec.Selector["kafka-cluster"] != "hotelflow" {
		t.Fatal("bootstrap must survive loss of one node")
	}
}
