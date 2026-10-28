package stand

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/kube"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRestartPreservesDeployment(t *testing.T) {
	replicas := int32(3)
	original := &apps.Deployment{
		ObjectMeta: meta.ObjectMeta{Name: "hotels", Namespace: "lab", ResourceVersion: "7"},
		Spec: apps.DeploymentSpec{
			Replicas: &replicas,
			Strategy: apps.DeploymentStrategy{Type: apps.RollingUpdateDeploymentStrategyType},
			Template: core.PodTemplateSpec{
				ObjectMeta: meta.ObjectMeta{Annotations: map[string]string{"keep": "yes"}},
				Spec:       core.PodSpec{Containers: []core.Container{{Name: "hotels", Image: "hotels:stable"}}},
			},
		},
	}
	client := fake.NewSimpleClientset(original)
	service := config.Service{Name: "hotels", Deployment: "hotels", Namespace: "lab"}
	if err := restartDeployment(context.Background(), &kube.Client{Typed: client}, service); err != nil {
		t.Fatal(err)
	}
	updated, err := client.AppsV1().Deployments("lab").Get(context.Background(), "hotels", meta.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Fatal("restart annotation missing")
	}
	delete(updated.Spec.Template.Annotations, "kubectl.kubernetes.io/restartedAt")
	if !reflect.DeepEqual(original.Spec, updated.Spec) {
		t.Fatal("restart changed replicas, strategy or pod configuration")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" && action.GetVerb() != "patch" {
			t.Fatalf("unexpected mutation: %s", action.GetVerb())
		}
	}
}

func TestRuntimeConfigStatus(t *testing.T) {
	tests := []struct {
		name, body, want string
		err              error
	}{
		{"ready", `{"started":"success","runtime_config":"success"}`, "success", nil},
		{"not ready", `{"started":"success","runtime_config":"waiting"}`, "waiting", errors.New("503")},
		{"omitted", `{"started":"success"}`, "не подключена", nil},
		{"offline", ``, "admin недоступен", errors.New("timeout")},
		{"invalid", `{}`, "неверный ответ admin", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runtimeConfigStatus([]byte(tt.body), tt.err); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
