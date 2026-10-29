package stand

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/canary"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/kube"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/project"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func (m *Manager) Canary(ctx context.Context, options canary.Options) error {
	if err := options.Validate(); err != nil {
		return err
	}
	if options.Action != "status" {
		unlock, err := m.Store.Lock()
		if err != nil {
			return err
		}
		defer unlock()
	}
	state, err := m.running(ctx)
	if err != nil {
		return err
	}
	if state.Reset != nil {
		return fmt.Errorf("сначала завершите hotelctl reset --yes")
	}
	c, err := kube.New(m.Config.Kubeconfig())
	if err != nil {
		return err
	}
	services, err := m.Config.Select([]string{"review", "hotels"})
	if err != nil {
		return err
	}
	for _, service := range services {
		if service.Namespace != canary.Namespace || service.Deployment != service.Name {
			return fmt.Errorf("канарейка ожидает стандартные Review и Hotels в hotelflow")
		}
	}
	if options.Action == "setup" {
		return m.setupReviewCanary(ctx, c, services)
	}
	r, err := c.ReviewRollout(ctx)
	if err != nil {
		return fmt.Errorf("сначала выполните hotelctl canary setup review: %w", err)
	}
	if options.Action == "status" {
		return m.printCanaryStatus(ctx, c, r)
	}
	switch options.Action {
	case "deploy":
		return m.deployReviewCanary(ctx, c, state, services[0], r, options.Restart)
	case "step":
		if err := kube.NextCanaryStep(r, options.Percent); err != nil {
			return err
		}
		if err := c.PatchReviewRollout(ctx, r, map[string]any{"status": map[string]any{"pauseConditions": nil}}, true); err != nil {
			return err
		}
		if err := waitCanaryPause(ctx, c, int64(options.Percent)); err != nil {
			return err
		}
	case "abort":
		stable, current := kube.RolloutHashes(r)
		if stable == "" || stable == current {
			return fmt.Errorf("нет активного канареечного релиза для отката")
		}
		if err := c.PatchReviewRollout(ctx, r, map[string]any{"status": map[string]any{"abort": true}}, true); err != nil {
			return err
		}
		if err := kube.Poll(ctx, "возврат трафика на stable", func(ctx context.Context) (bool, error) {
			stable, candidate, err := c.CanaryWeights(ctx)
			return stable == 100 && candidate == 0, err
		}); err != nil {
			return err
		}
		fmt.Fprintln(m.Streams.Out, "Релиз отменён. Spec сохраняет отклонённую версию; следующий deploy должен содержать исправленный образ.")
	case "promote":
		if err := requireFinalPause(r); err != nil {
			return err
		}
		if err := c.PatchReviewRollout(ctx, r, map[string]any{"status": map[string]any{"pauseConditions": nil}}, true); err != nil {
			return err
		}
		if err := waitReviewHealthy(ctx, c); err != nil {
			return err
		}
	case "disable":
		return m.disableReviewCanary(ctx, c, state, services, r)
	}
	r, err = c.ReviewRollout(ctx)
	if err != nil {
		return err
	}
	return m.printCanaryStatus(ctx, c, r)
}

func (m *Manager) setupReviewCanary(ctx context.Context, c *kube.Client, services []config.Service) error {
	r, err := c.ReviewRollout(ctx)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if r != nil {
		// Repeating setup must not reset weights or a running release.
		enabled := r.GetAnnotations()["hotelflow/canary-enabled"] == "true"
		if enabled {
			return m.printCanaryStatus(ctx, c, r)
		}
	} else {
		for _, service := range services {
			if err := c.WaitDeployment(ctx, service); err != nil {
				return err
			}
		}
		fmt.Fprintln(m.Streams.Out, "Установка Istio 1.31.3 (minimal) и Argo Rollouts v1.10.1")
		install := exec.CommandContext(ctx, "bash", filepath.Join(m.Config.Root, "deploy/canary/install.sh"), m.Config.Kubeconfig(), filepath.Join(m.Config.StateDir(), "tools"))
		install.Stdout, install.Stderr = m.Streams.Out, m.Streams.ErrOut
		if err := install.Run(); err != nil {
			return fmt.Errorf("установка контроллеров: %w", err)
		}
		review, err := c.Typed.AppsV1().Deployments(canary.Namespace).Get(ctx, "review", metav1.GetOptions{})
		if err != nil {
			return err
		}
		hotels, err := c.Typed.AppsV1().Deployments(canary.Namespace).Get(ctx, "hotels", metav1.GetOptions{})
		if err != nil {
			return err
		}
		if review.Spec.Replicas == nil || *review.Spec.Replicas < 1 {
			return fmt.Errorf("перед setup Review должен быть запущен")
		}
		r, err = kube.RenderReviewRollout(m.Config, review, hotels)
		if err != nil {
			return err
		}
		routes, err := kube.Render(m.Config.Root, config.Service{Manifests: "deploy/canary"}, "")
		if err != nil {
			return err
		}
		if err := c.Apply(ctx, append(routes, r), m.Streams.Out); err != nil {
			return err
		}
	}
	if err := waitReviewHealthy(ctx, c); err != nil {
		return err
	}
	fmt.Fprintln(m.Streams.Out, "Переключение Hotels → Review на Istio; остальные связи не меняются")
	if err := c.EnableHotelsMesh(ctx); err != nil {
		return err
	}
	if err := c.WaitDeployment(ctx, services[1]); err != nil {
		return err
	}
	if err := requireHotelsSidecars(ctx, c); err != nil {
		return err
	}
	// Keep the legacy Deployment at zero for an explicit, reversible mode change.
	if err := c.Scale(ctx, services[0], 0); err != nil {
		return err
	}
	if err := c.WaitStopped(ctx, services[0]); err != nil {
		return err
	}
	r, err = c.ReviewRollout(ctx)
	if err != nil {
		return err
	}
	patch := []byte(`{"metadata":{"annotations":{"hotelflow/canary-enabled":"true"}}}`)
	_, err = c.Dynamic.Resource(kube.RolloutResource).Namespace(canary.Namespace).Patch(ctx, "review", "application/merge-patch+json", patch, metav1.PatchOptions{})
	if err != nil {
		return err
	}
	return m.printCanaryStatus(ctx, c, r)
}

func (m *Manager) deployReviewCanary(ctx context.Context, c *kube.Client, state *project.State, review config.Service, r *unstructured.Unstructured, restart bool) error {
	if r.GetAnnotations()["hotelflow/canary-enabled"] != "true" {
		return fmt.Errorf("завершите canary setup review")
	}
	stable, current := kube.RolloutHashes(r)
	aborted, _, _ := unstructured.NestedBool(r.Object, "status", "abort")
	if stable == "" || (stable != current && !aborted) {
		return fmt.Errorf("текущий релиз ещё не завершён; используйте step, abort или promote")
	}
	image := state.Images[review.Name]
	if image == "" {
		return fmt.Errorf("сначала выполните hotelctl build review")
	}
	containers, _, _ := unstructured.NestedSlice(r.Object, "spec", "template", "spec", "containers")
	found := false
	for _, raw := range containers {
		container, ok := raw.(map[string]any)
		if !ok || container["name"] != "review" {
			continue
		}
		if container["image"] == image && !restart {
			return fmt.Errorf("образ не изменился; соберите новый или укажите --restart для повторного релиза того же образа")
		}
		container["image"] = image
		found = true
	}
	if !found {
		return fmt.Errorf("Rollout не содержит контейнер review")
	}
	if err := m.Runtime.Import(ctx, state, image, m.Config.StateDir(), m.Streams.Out); err != nil {
		return err
	}
	template := map[string]any{"spec": map[string]any{"containers": containers}}
	if restart {
		template["metadata"] = map[string]any{"annotations": map[string]string{"hotelflow/canary-restart": time.Now().UTC().Format(time.RFC3339Nano)}}
	}
	if err := c.PatchReviewRollout(ctx, r, map[string]any{"spec": map[string]any{"template": template}}, false); err != nil {
		return err
	}
	if err := waitCanaryPause(ctx, c, 0); err != nil {
		return err
	}
	fmt.Fprintln(m.Streams.Out, "Canary готова, вес 0%. Следующий шаг: hotelctl canary step review 10")
	return nil
}

func waitReviewHealthy(ctx context.Context, c *kube.Client) error {
	return kube.Poll(ctx, "готовность стабильной ревизии Review", func(ctx context.Context) (bool, error) {
		r, err := c.ReviewRollout(ctx)
		if err != nil {
			return false, err
		}
		stable, current := kube.RolloutHashes(r)
		phase, _, _ := unstructured.NestedString(r.Object, "status", "phase")
		return stable != "" && stable == current && phase == "Healthy", nil
	})
}

func waitCanaryPause(ctx context.Context, c *kube.Client, weight int64) error {
	return kube.Poll(ctx, "ручной шаг Review и веса Istio", func(ctx context.Context) (bool, error) {
		r, err := c.ReviewRollout(ctx)
		if err != nil {
			return false, err
		}
		abort, _, _ := unstructured.NestedBool(r.Object, "status", "abort")
		stable, current := kube.RolloutHashes(r)
		conditions, _, _ := unstructured.NestedSlice(r.Object, "status", "pauseConditions")
		if abort || stable == "" || stable == current || len(conditions) == 0 {
			return false, nil
		}
		index, _, _ := unstructured.NestedInt64(r.Object, "status", "currentStepIndex")
		steps, _, _ := unstructured.NestedSlice(r.Object, "spec", "strategy", "canary", "steps")
		var declared int64 = -1
		for i := int64(0); i <= index && i < int64(len(steps)); i++ {
			step, ok := steps[i].(map[string]any)
			if !ok {
				continue
			}
			if value, ok := step["setWeight"].(int64); ok {
				declared = value
			}
		}
		_, actual, err := c.CanaryWeights(ctx)
		return declared == weight && actual == weight, err
	})
}

func requireFinalPause(r *unstructured.Unstructured) error {
	abort, _, _ := unstructured.NestedBool(r.Object, "status", "abort")
	index, _, _ := unstructured.NestedInt64(r.Object, "status", "currentStepIndex")
	steps, _, _ := unstructured.NestedSlice(r.Object, "spec", "strategy", "canary", "steps")
	pauses, _, _ := unstructured.NestedSlice(r.Object, "status", "pauseConditions")
	if abort || len(pauses) == 0 || len(steps) == 0 || index != int64(len(steps)-1) {
		return fmt.Errorf("сначала последовательно пройдите шаги 10, 25, 50, 100; promote завершает только последний шаг")
	}
	return nil
}

func requireHotelsSidecars(ctx context.Context, c *kube.Client) error {
	pods, err := c.Typed.CoreV1().Pods(canary.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=hotels"})
	if err != nil {
		return err
	}
	found := 0
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}
		ready := false
		statuses := append(pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses...)
		for _, container := range statuses {
			if container.Name == "istio-proxy" && container.Ready {
				ready = true
			}
		}
		if !ready {
			return fmt.Errorf("pod %s не содержит готовый istio-proxy; проверьте injector и повторите setup", pod.Name)
		}
		found++
	}
	if found == 0 {
		return fmt.Errorf("нет pod Hotels для проверки Istio")
	}
	return nil
}

func (m *Manager) printCanaryStatus(ctx context.Context, c *kube.Client, r *unstructured.Unstructured) error {
	stable, current := kube.RolloutHashes(r)
	phase, _, _ := unstructured.NestedString(r.Object, "status", "phase")
	message, _, _ := unstructured.NestedString(r.Object, "status", "message")
	stableWeight, canaryWeight, err := c.CanaryWeights(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(m.Streams.Out, "\nReview · %s · %s\nВеса Istio: stable %d%% / canary %d%% (это не измеренная доля RPC)\n%s\n", time.Now().Format("15:04:05"), phase, stableWeight, canaryWeight, message)
	sets, err := c.Typed.AppsV1().ReplicaSets(canary.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=" + canary.Service})
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(m.Streams.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "REPLICASET\tROLE\tREADY\tIMAGE")
	for _, rs := range sets.Items {
		owned := false
		for _, owner := range rs.OwnerReferences {
			if owner.UID == r.GetUID() {
				owned = true
			}
		}
		if !owned {
			continue
		}
		role := "previous"
		hash := rs.Labels["rollouts-pod-template-hash"]
		if hash == current && current != stable {
			role = "canary"
		} else if hash == stable {
			role = "stable"
		}
		image, err := kube.ImageOf(rs.Spec.Template.Spec.Containers, "review")
		if err != nil {
			return err
		}
		replicas := int32(0)
		if rs.Spec.Replicas != nil {
			replicas = *rs.Spec.Replicas
		}
		fmt.Fprintf(w, "%s\t%s\t%d/%d\t%s\n", rs.Name, role, rs.Status.ReadyReplicas, replicas, image)
	}
	return w.Flush()
}

func (m *Manager) disableReviewCanary(ctx context.Context, c *kube.Client, state *project.State, services []config.Service, r *unstructured.Unstructured) error {
	backup, err := kube.BackupOf(r)
	if err != nil {
		return err
	}
	stable, current := kube.RolloutHashes(r)
	aborted, _, _ := unstructured.NestedBool(r.Object, "status", "abort")
	if stable != current && !aborted {
		return fmt.Errorf("перед disable завершите canary abort/promote review")
	}
	template, err := c.StableReviewTemplate(ctx, r)
	if err != nil {
		return err
	}
	image, err := kube.ImageOf(template.Spec.Containers, "review")
	if err != nil {
		return err
	}
	d, err := c.Typed.AppsV1().Deployments(canary.Namespace).Get(ctx, "review", metav1.GetOptions{})
	if err != nil {
		return err
	}
	for i := range d.Spec.Template.Spec.Containers {
		if d.Spec.Template.Spec.Containers[i].Name == "review" {
			d.Spec.Template.Spec.Containers[i].Image = image
		}
	}
	d.Spec.Replicas = &backup.ReviewReplicas
	if _, err := c.Typed.AppsV1().Deployments(canary.Namespace).Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		return err
	}
	if err := c.WaitDeployment(ctx, services[0]); err != nil {
		return err
	}
	if err := c.RestoreHotelsMesh(ctx, backup); err != nil {
		return err
	}
	if err := c.WaitDeployment(ctx, services[1]); err != nil {
		return err
	}
	state.Images["review"] = image
	if err := m.Store.Save(state); err != nil {
		return err
	}
	propagation := metav1.DeletePropagationForeground
	if err := c.Dynamic.Resource(kube.RolloutResource).Namespace(canary.Namespace).Delete(ctx, "review", metav1.DeleteOptions{PropagationPolicy: &propagation}); err != nil {
		return err
	}
	if err := kube.Poll(ctx, "завершение Review Rollout", func(ctx context.Context) (bool, error) {
		_, err := c.ReviewRollout(ctx)
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}); err != nil {
		return err
	}
	for _, resource := range []struct {
		kind   string
		remove func() error
	}{
		{"VirtualService", func() error {
			return c.Dynamic.Resource(kube.VirtualServiceResource).Namespace(canary.Namespace).Delete(ctx, canary.Routing, metav1.DeleteOptions{})
		}},
		{"DestinationRule", func() error {
			return c.Dynamic.Resource(kube.DestinationRuleResource).Namespace(canary.Namespace).Delete(ctx, canary.Routing, metav1.DeleteOptions{})
		}},
		{"Service", func() error {
			return c.Typed.CoreV1().Services(canary.Namespace).Delete(ctx, canary.Service, metav1.DeleteOptions{})
		}},
	} {
		if err := resource.remove(); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("удаление %s: %w", resource.kind, err)
		}
	}
	fmt.Fprintln(m.Streams.Out, "Канареечный режим выключен. Hotels → Envoy → Review; стабильный образ сохранён. Контроллеры оставлены для следующего setup.")
	return nil
}
