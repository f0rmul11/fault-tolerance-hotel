package kube

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/canary"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func canaryFixture(t *testing.T) (*unstructured.Unstructured, *appsv1.Deployment, *appsv1.Deployment) {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	load := func(name string) *appsv1.Deployment {
		t.Helper()
		services, err := c.Select([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		objects, err := RenderService(c, services[0], "hotelflow/"+name+":stable")
		if err != nil {
			t.Fatal(err)
		}
		for _, object := range objects {
			if object.GetKind() == "Deployment" {
				var d appsv1.Deployment
				raw, err := object.MarshalJSON()
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(raw, &d); err != nil {
					t.Fatal(err)
				}
				return &d
			}
		}
		t.Fatal("Deployment missing")
		return nil
	}
	review, hotels := load("review"), load("hotels")
	r, err := RenderReviewRollout(c, review, hotels)
	if err != nil {
		t.Fatal(err)
	}
	return r, review, hotels
}

func TestReviewRolloutPreservesApplicationAndSeparatesSelectors(t *testing.T) {
	r, review, _ := canaryFixture(t)
	raw, _, err := unstructured.NestedMap(r.Object, "spec", "template")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var template corev1.PodTemplateSpec
	if err := json.Unmarshal(data, &template); err != nil {
		t.Fatal(err)
	}
	for i := range review.Spec.Template.Spec.Containers {
		for j := range review.Spec.Template.Spec.Containers[i].Ports {
			review.Spec.Template.Spec.Containers[i].Ports[j].Protocol = corev1.ProtocolTCP
		}
	}
	if !reflect.DeepEqual(template.Spec.Containers, review.Spec.Template.Spec.Containers) {
		t.Fatal("migration changed app image, probes, resources or database configuration")
	}
	if template.Labels["app.kubernetes.io/name"] == review.Spec.Template.Labels["app.kubernetes.io/name"] {
		t.Fatal("legacy Deployment would select Rollout pods")
	}
	if template.Labels["sidecar.istio.io/inject"] != "true" {
		t.Fatal("Review sidecar missing")
	}
	backup, err := BackupOf(r)
	if err != nil || backup.ReviewReplicas != *review.Spec.Replicas {
		t.Fatalf("migration backup: %v", err)
	}
}

func TestHotelsMeshRestorationKeepsUnrelatedEdits(t *testing.T) {
	r, _, hotels := canaryFixture(t)
	before := hotels.DeepCopy()
	c := &Client{Typed: kubefake.NewClientset(hotels)}
	ctx := context.Background()
	if err := c.EnableHotelsMesh(ctx); err != nil {
		t.Fatal(err)
	}
	updated, err := c.Typed.AppsV1().Deployments(canary.Namespace).Get(ctx, "hotels", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	target := ""
	for _, env := range updated.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "SERVICE_TARGETS" {
			target = env.Value
		}
	}
	if !strings.Contains(target, "review:review-rollout.hotelflow.svc.cluster.local:9090") {
		t.Fatal("Review still bypasses Istio")
	}
	updated.Spec.Template.Spec.Containers[0].Image = "hotelflow/hotels:independent-change"
	updated.Spec.Template.Labels["unrelated"] = "keep"
	if _, err := c.Typed.AppsV1().Deployments(canary.Namespace).Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	backup, err := BackupOf(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.RestoreHotelsMesh(ctx, backup); err != nil {
		t.Fatal(err)
	}
	restored, err := c.Typed.AppsV1().Deployments(canary.Namespace).Get(ctx, "hotels", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Spec.Template.Spec.Containers[0].Image != "hotelflow/hotels:independent-change" || restored.Spec.Template.Labels["unrelated"] != "keep" {
		t.Fatal("restore overwrote independent changes")
	}
	if !reflect.DeepEqual(before.Spec.Template.Spec.Containers[0].Env, restored.Spec.Template.Spec.Containers[0].Env) {
		// SERVICE_TARGETS may move to the end; compare by name rather than order.
		envMap := func(values []corev1.EnvVar) map[string]corev1.EnvVar {
			m := map[string]corev1.EnvVar{}
			for _, value := range values {
				m[value.Name] = value
			}
			return m
		}
		if !reflect.DeepEqual(envMap(before.Spec.Template.Spec.Containers[0].Env), envMap(restored.Spec.Template.Spec.Containers[0].Env)) {
			t.Fatal("original service routes not restored")
		}
	}
}

func TestCanaryStepsCannotBypassPauses(t *testing.T) {
	r, _, _ := canaryFixture(t)
	for _, test := range []struct {
		name      string
		index     int64
		percent   int
		abort     bool
		paused    bool
		wantError bool
	}{
		{name: "first step", index: 2, percent: 10, paused: true},
		{name: "next step", index: 5, percent: 25, paused: true},
		{name: "skip gate", index: 2, percent: 100, paused: true, wantError: true},
		{name: "not ready yet", index: 4, percent: 25, wantError: true},
		{name: "aborted", index: 5, percent: 25, abort: true, paused: true, wantError: true},
		{name: "final pause", index: 11, percent: 100, paused: true, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			object := r.DeepCopy()
			status := map[string]any{
				"stableRS":         "old",
				"currentPodHash":   "new",
				"currentStepIndex": test.index,
				"abort":            test.abort,
			}
			if test.paused {
				status["pauseConditions"] = []any{map[string]any{"reason": "CanaryPauseStep"}}
			}
			object.Object["status"] = status
			err := NextCanaryStep(object, test.percent)
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestExistingRolloutBlocksOrdinaryDeployment(t *testing.T) {
	r, _, _ := canaryFixture(t)
	c := &Client{Dynamic: fake.NewSimpleDynamicClient(runtime.NewScheme(), r)}
	if err := c.GuardReviewCanary(t.Context()); err == nil {
		t.Fatal("ordinary deploy can bypass Argo")
	}
	if err := c.Dynamic.Resource(RolloutResource).Namespace(canary.Namespace).Delete(t.Context(), "review", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := c.GuardReviewCanary(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestReviewRolloutUsesLocalTemplateInsteadOfLiveDeployment(t *testing.T) {
	_, review, hotels := canaryFixture(t)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Root = t.TempDir()
	path := filepath.Join(cfg.Root, "services/review/build/canary")
	if err := os.CopyFS(path, os.DirFS(filepath.Join(root, "services/review/build/canary"))); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(path, "rollout.yaml")
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "periodSeconds: 2", "periodSeconds: 7", 1))
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	review.Spec.Template.Spec.Containers[0].ReadinessProbe.PeriodSeconds = 99
	cfg.Observability.Enabled = false
	rollout, err := RenderReviewRollout(cfg, review, hotels)
	if err != nil {
		t.Fatal(err)
	}
	containers, _, _ := unstructured.NestedSlice(rollout.Object, "spec", "template", "spec", "containers")
	container := containers[0].(map[string]any)
	period, _, _ := unstructured.NestedInt64(container, "readinessProbe", "periodSeconds")
	if period != 7 {
		t.Fatalf("local readiness period lost: %d", period)
	}
	if container["image"] != review.Spec.Template.Spec.Containers[0].Image {
		t.Fatal("setup did not preserve stable image")
	}
	env, _, _ := unstructured.NestedSlice(container, "env")
	for _, raw := range env {
		entry := raw.(map[string]any)
		if entry["name"] == "TELEMETRY_ENABLED" && entry["value"] != "false" {
			t.Fatal("stand telemetry setting ignored")
		}
	}
}

func TestCanaryInfrastructureRendersOffline(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, component := range []struct{ path, namespace, image string }{
		{path: "deploy/istio", namespace: "istio-system", image: "docker.io/istio/pilot:1.31.3"},
		{path: "deploy/argo-rollouts", namespace: "argo-rollouts", image: "quay.io/argoproj/argo-rollouts:v1.10.1"},
	} {
		for _, stage := range []string{"crds", "controller"} {
			objects, err := Render(root, config.Service{Manifests: component.path + "/" + stage}, "")
			if err != nil {
				t.Fatal(err)
			}
			deployments := 0
			for _, object := range objects {
				if stage == "crds" && object.GetKind() != "Namespace" && object.GetKind() != "CustomResourceDefinition" {
					t.Fatal("controller resource applied before CRDs")
				}
				if object.GetKind() == "Deployment" {
					deployments++
					if object.GetNamespace() != component.namespace {
						t.Fatal("wrong controller namespace")
					}
					containers, _, _ := unstructured.NestedSlice(object.Object, "spec", "template", "spec", "containers")
					if containers[0].(map[string]any)["image"] != component.image {
						t.Fatal("controller image is not pinned")
					}
				}
				if object.GetKind() == "IstioOperator" {
					t.Fatal("runtime must not depend on Istio operator")
				}
			}
			if stage == "controller" && deployments != 1 {
				t.Fatalf("expected one controller, got %d", deployments)
			}
		}
	}
}
