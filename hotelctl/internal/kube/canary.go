package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/canary"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kjson "k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/yaml"
)

var RolloutResource = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "rollouts"}
var VirtualServiceResource = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "virtualservices"}
var DestinationRuleResource = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "destinationrules"}

const backupAnnotation = "hotelflow/canary-backup"

type MeshBackup struct {
	HotelsTemplate corev1.PodTemplateSpec `json:"hotelsTemplate"`
	ReviewReplicas int32                  `json:"reviewReplicas"`
}

func (c *Client) ReviewRollout(ctx context.Context) (*unstructured.Unstructured, error) {
	return c.Dynamic.Resource(RolloutResource).Namespace(canary.Namespace).Get(ctx, canary.Name, metav1.GetOptions{})
}

// Guard a mode, not just an active update: ordinary deploy must not bypass Rollouts.
func (c *Client) GuardReviewCanary(ctx context.Context) error {
	_, err := c.ReviewRollout(ctx)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("Review управляется Argo Rollouts; используйте hotelctl canary или сначала canary disable review")
}

func (c *Client) PatchReviewRollout(ctx context.Context, object *unstructured.Unstructured, patch map[string]any, status bool) error {
	patch["metadata"] = map[string]any{"resourceVersion": object.GetResourceVersion()}
	body, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	var subresources []string
	if status {
		subresources = []string{"status"}
	}
	_, err = c.Dynamic.Resource(RolloutResource).Namespace(canary.Namespace).Patch(ctx, canary.Name, types.MergePatchType, body, metav1.PatchOptions{}, subresources...)
	return err
}

func ImageOf(containers []corev1.Container, name string) (string, error) {
	for _, container := range containers {
		if container.Name == name {
			return container.Image, nil
		}
	}
	return "", fmt.Errorf("не найден контейнер %s", name)
}

func RenderReviewRollout(c config.Config, stable *appsv1.Deployment, hotels *appsv1.Deployment) (*unstructured.Unstructured, error) {
	// Use the live stable template so bootstrap cannot accidentally deploy a new local build.
	template := stable.Spec.Template.DeepCopy()
	if template.Labels == nil {
		template.Labels = map[string]string{}
	}
	template.Labels["app.kubernetes.io/name"] = canary.Service
	enableMesh(template)
	podObject, err := runtime.DefaultUnstructuredConverter.ToUnstructured(template)
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(filepath.Join(c.Root, "deploy/canary/strategy.yaml"))
	if err != nil {
		return nil, err
	}
	data, err := yaml.YAMLToJSON(body)
	if err != nil {
		return nil, err
	}
	spec := map[string]any{}
	if err := kjson.Unmarshal(data, &spec); err != nil {
		return nil, err
	}
	spec["selector"] = map[string]any{"matchLabels": map[string]any{"app.kubernetes.io/name": canary.Service}}
	spec["template"] = podObject
	replicas := int32(1)
	if stable.Spec.Replicas != nil {
		replicas = *stable.Spec.Replicas
	}
	backup, err := json.Marshal(MeshBackup{HotelsTemplate: hotels.Spec.Template, ReviewReplicas: replicas})
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Rollout",
		"metadata": map[string]any{
			"name":        canary.Name,
			"namespace":   canary.Namespace,
			"annotations": map[string]any{backupAnnotation: string(backup)},
		},
		"spec": spec,
	}}, nil
}

func enableMesh(template *corev1.PodTemplateSpec) {
	if template.Labels == nil {
		template.Labels = map[string]string{}
	}
	if template.Annotations == nil {
		template.Annotations = map[string]string{}
	}
	template.Labels["sidecar.istio.io/inject"] = "true"
	template.Annotations["proxy.istio.io/config"] = `{"holdApplicationUntilProxyStarts":true,"terminationDrainDuration":"35s"}`
	// Explicit ports are captured before the IP-range fallback. Empty ranges
	// keep PostgreSQL, etcd, telemetry and existing Envoy routes outside Istio.
	template.Annotations["traffic.sidecar.istio.io/includeOutboundIPRanges"] = ""
	template.Annotations["traffic.sidecar.istio.io/includeOutboundPorts"] = "9090"
	template.Annotations["traffic.sidecar.istio.io/includeInboundPorts"] = "9090"
	grace := int64(45)
	template.Spec.TerminationGracePeriodSeconds = &grace
}

func (c *Client) EnableHotelsMesh(ctx context.Context) error {
	d, err := c.Typed.AppsV1().Deployments(canary.Namespace).Get(ctx, "hotels", metav1.GetOptions{})
	if err != nil {
		return err
	}
	enableMesh(&d.Spec.Template)
	for i := range d.Spec.Template.Spec.Containers {
		container := &d.Spec.Template.Spec.Containers[i]
		if container.Name != "hotels" {
			continue
		}
		for j := range container.Env {
			if container.Env[j].Name != "SERVICE_TARGETS" {
				continue
			}
			var targets []string
			for _, target := range strings.Split(container.Env[j].Value, ",") {
				if target != "" && !strings.HasPrefix(target, "review:") {
					targets = append(targets, target)
				}
			}
			targets = append(targets, "review:review-rollout.hotelflow.svc.cluster.local:9090")
			container.Env[j].Value = strings.Join(targets, ",")
			container.Env[j].ValueFrom = nil
			_, err = c.Typed.AppsV1().Deployments(canary.Namespace).Update(ctx, d, metav1.UpdateOptions{})
			return err
		}
		container.Env = append(container.Env, corev1.EnvVar{Name: "SERVICE_TARGETS", Value: "review:review-rollout.hotelflow.svc.cluster.local:9090"})
		_, err = c.Typed.AppsV1().Deployments(canary.Namespace).Update(ctx, d, metav1.UpdateOptions{})
		return err
	}
	return fmt.Errorf("в Hotels не найден контейнер hotels")
}

func BackupOf(r *unstructured.Unstructured) (MeshBackup, error) {
	var backup MeshBackup
	err := json.Unmarshal([]byte(r.GetAnnotations()[backupAnnotation]), &backup)
	if err != nil || backup.ReviewReplicas < 1 || len(backup.HotelsTemplate.Spec.Containers) == 0 {
		return backup, fmt.Errorf("Rollout не содержит корректного состояния для canary disable")
	}
	return backup, nil
}

func (c *Client) RestoreHotelsMesh(ctx context.Context, backup MeshBackup) error {
	d, err := c.Typed.AppsV1().Deployments(canary.Namespace).Get(ctx, "hotels", metav1.GetOptions{})
	if err != nil {
		return err
	}
	// These fields alone were changed by setup. Keep images and unrelated live edits.
	for _, key := range []string{"proxy.istio.io/config", "traffic.sidecar.istio.io/includeOutboundPorts", "traffic.sidecar.istio.io/includeOutboundIPRanges", "traffic.sidecar.istio.io/includeInboundPorts"} {
		delete(d.Spec.Template.Annotations, key)
		if value, ok := backup.HotelsTemplate.Annotations[key]; ok {
			d.Spec.Template.Annotations[key] = value
		}
	}
	delete(d.Spec.Template.Labels, "sidecar.istio.io/inject")
	if value, ok := backup.HotelsTemplate.Labels["sidecar.istio.io/inject"]; ok {
		d.Spec.Template.Labels["sidecar.istio.io/inject"] = value
	}
	d.Spec.Template.Spec.TerminationGracePeriodSeconds = backup.HotelsTemplate.Spec.TerminationGracePeriodSeconds
	for i := range d.Spec.Template.Spec.Containers {
		container := &d.Spec.Template.Spec.Containers[i]
		if container.Name != "hotels" {
			continue
		}
		var env []corev1.EnvVar
		for _, item := range container.Env {
			if item.Name != "SERVICE_TARGETS" {
				env = append(env, item)
			}
		}
		for _, old := range backup.HotelsTemplate.Spec.Containers {
			if old.Name == "hotels" {
				for _, item := range old.Env {
					if item.Name == "SERVICE_TARGETS" {
						env = append(env, item)
					}
				}
			}
		}
		container.Env = env
	}
	_, err = c.Typed.AppsV1().Deployments(canary.Namespace).Update(ctx, d, metav1.UpdateOptions{})
	return err
}

func (c *Client) CanaryWeights(ctx context.Context) (int64, int64, error) {
	vs, err := c.Dynamic.Resource(VirtualServiceResource).Namespace(canary.Namespace).Get(ctx, canary.Routing, metav1.GetOptions{})
	if err != nil {
		return 0, 0, err
	}
	routes, _, err := unstructured.NestedSlice(vs.Object, "spec", "http")
	if err != nil {
		return 0, 0, err
	}
	weights := map[string]int64{}
	for _, raw := range routes {
		route, ok := raw.(map[string]any)
		if !ok || route["name"] != "primary" {
			continue
		}
		destinations, _, _ := unstructured.NestedSlice(route, "route")
		for _, raw := range destinations {
			target, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			subset, _, _ := unstructured.NestedString(target, "destination", "subset")
			weight, _, _ := unstructured.NestedInt64(target, "weight")
			weights[subset] = weight
		}
	}
	stable, stableOK := weights["stable"]
	candidate, canaryOK := weights["canary"]
	if !stableOK || !canaryOK || stable+candidate != 100 {
		return 0, 0, fmt.Errorf("неверные веса в VirtualService review-canary")
	}
	return stable, candidate, nil
}

func RolloutHashes(r *unstructured.Unstructured) (string, string) {
	stable, _, _ := unstructured.NestedString(r.Object, "status", "stableRS")
	current, _, _ := unstructured.NestedString(r.Object, "status", "currentPodHash")
	return stable, current
}

func (c *Client) StableReviewTemplate(ctx context.Context, r *unstructured.Unstructured) (*corev1.PodTemplateSpec, error) {
	stable, _ := RolloutHashes(r)
	if stable == "" {
		return nil, fmt.Errorf("стабильная ревизия Review ещё не определена")
	}
	sets, err := c.Typed.AppsV1().ReplicaSets(canary.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=" + canary.Service + ",rollouts-pod-template-hash=" + stable})
	if err != nil {
		return nil, err
	}
	for _, set := range sets.Items {
		for _, owner := range set.OwnerReferences {
			if owner.UID == r.GetUID() {
				return set.Spec.Template.DeepCopy(), nil
			}
		}
	}
	return nil, fmt.Errorf("стабильный ReplicaSet Review не найден")
}

// NextCanaryStep validates the pause before allowing the controller to advance it.
func NextCanaryStep(r *unstructured.Unstructured, requested int) error {
	abort, _, _ := unstructured.NestedBool(r.Object, "status", "abort")
	if abort {
		return fmt.Errorf("релиз отменён; загрузите исправленный образ через canary deploy")
	}
	stable, current := RolloutHashes(r)
	if stable == "" || current == "" || stable == current {
		return fmt.Errorf("нет новой канареечной ревизии")
	}
	conditions, _, _ := unstructured.NestedSlice(r.Object, "status", "pauseConditions")
	if len(conditions) != 1 {
		return fmt.Errorf("Rollout ещё не остановился на ручном шаге")
	}
	condition, ok := conditions[0].(map[string]any)
	if !ok || condition["reason"] != "CanaryPauseStep" {
		return fmt.Errorf("Rollout приостановлен не на шаге канарейки")
	}
	index, _, _ := unstructured.NestedInt64(r.Object, "status", "currentStepIndex")
	steps, _, err := unstructured.NestedSlice(r.Object, "spec", "strategy", "canary", "steps")
	if err != nil {
		return err
	}
	if index < 0 || index >= int64(len(steps)) {
		return fmt.Errorf("некорректный индекс шага Rollout")
	}
	for i := int(index) + 1; i < len(steps); i++ {
		step, ok := steps[i].(map[string]any)
		if !ok {
			continue
		}
		value, found, _ := unstructured.NestedInt64(step, "setWeight")
		if found {
			if value != int64(requested) {
				return fmt.Errorf("следующий шаг — %d%%; выполните canary step review %d", value, value)
			}
			return nil
		}
	}
	return fmt.Errorf("последний шаг пройден; завершите canary promote review")
}
