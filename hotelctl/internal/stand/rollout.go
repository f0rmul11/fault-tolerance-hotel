package stand

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/kube"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (m *Manager) RolloutRestart(ctx context.Context, service config.Service) error {
	unlock, err := m.Store.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	state, err := m.running(ctx)
	if err != nil {
		return err
	}
	if state.Reset != nil {
		return fmt.Errorf("сначала завершите reset")
	}
	c, err := kube.New(m.Config.Kubeconfig())
	if err != nil {
		return err
	}
	if err := restartDeployment(ctx, c, service); err != nil {
		return err
	}

	fmt.Fprintf(m.Streams.Out, "Обновление %s запущено. Наблюдение: hotelctl status %s --watch\n", service.Name, service.Name)
	return nil
}

func restartDeployment(ctx context.Context, c *kube.Client, service config.Service) error {
	d, err := c.Typed.AppsV1().Deployments(service.Namespace).Get(ctx, service.Deployment, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if d.Spec.Paused {
		return fmt.Errorf("Deployment приостановлен")
	}
	if d.Spec.Replicas != nil && *d.Spec.Replicas == 0 {
		return fmt.Errorf("%s остановлен; выполните hotelctl start %s", service.Name, service.Name)
	}
	patch, err := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"resourceVersion": d.ResourceVersion,
		},
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]string{
						"kubectl.kubernetes.io/restartedAt": time.Now().UTC().Format(time.RFC3339Nano),
					},
				},
			},
		},
	})
	if err != nil {
		return err
	}
	_, err = c.Typed.AppsV1().Deployments(service.Namespace).Patch(ctx, service.Deployment, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return err
	}
	return nil
}

type podRow struct {
	Pod           string `json:"pod"`
	Revision      string `json:"revision"`
	Phase         string `json:"phase"`
	Ready         bool   `json:"ready"`
	RuntimeConfig string `json:"runtimeConfig"`
	Restarts      int32  `json:"restarts"`
}

func (m *Manager) PodStatus(ctx context.Context, service config.Service, format string) error {
	if _, err := m.running(ctx); err != nil {
		return err
	}
	c, err := kube.New(m.Config.Kubeconfig())
	if err != nil {
		return err
	}
	d, err := c.Typed.AppsV1().Deployments(service.Namespace).Get(ctx, service.Deployment, metav1.GetOptions{})
	if err != nil {
		return err
	}
	selector, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil {
		return err
	}
	sets, err := c.Typed.AppsV1().ReplicaSets(service.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return err
	}
	revisions := map[types.UID]string{}
	for _, rs := range sets.Items {
		for _, owner := range rs.OwnerReferences {
			if owner.UID == d.UID {
				revisions[rs.UID] = rs.Annotations["deployment.kubernetes.io/revision"]
			}
		}
	}
	pods, err := c.Typed.CoreV1().Pods(service.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return err
	}
	rows := []podRow{}
	for _, pod := range pods.Items {
		revision := ""
		owned := false
		for _, owner := range pod.OwnerReferences {
			if v, ok := revisions[owner.UID]; ok {
				revision = v
				owned = true
			}
		}
		if !owned {
			continue
		}
		row := podRow{
			Pod:           pod.Name,
			Revision:      revision,
			Phase:         string(pod.Status.Phase),
			RuntimeConfig: "admin недоступен",
		}
		if pod.DeletionTimestamp != nil {
			row.Phase = "Terminating"
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady {
				row.Ready = condition.Status == corev1.ConditionTrue
			}
		}
		for _, container := range pod.Status.ContainerStatuses {
			row.Restarts += container.RestartCount
		}
		probeCtx, cancel := context.WithTimeout(ctx, time.Second)
		data, probeErr := c.Typed.CoreV1().RESTClient().Get().Namespace(service.Namespace).Resource("pods").Name(pod.Name + ":8081").SubResource("proxy").Suffix("healthcheck/ready").DoRaw(probeCtx)
		cancel()
		row.RuntimeConfig = runtimeConfigStatus(data, probeErr)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Pod < rows[j].Pod
	})
	if format == "json" {
		return json.NewEncoder(m.Streams.Out).Encode(rows)
	}
	fmt.Fprintf(m.Streams.Out, "\n%s — %s\n", service.Name, time.Now().Format("15:04:05"))
	w := tabwriter.NewWriter(m.Streams.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "POD\tREVISION\tSTATUS\tREADY\tRTCONFIG\tRESTARTS")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%t\t%s\t%d\n", row.Pod, row.Revision, row.Phase, row.Ready, row.RuntimeConfig, row.Restarts)
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "Нет Pod")
	}
	return w.Flush()
}

func runtimeConfigStatus(data []byte, probeErr error) string {
	checks := map[string]string{}
	if json.Unmarshal(data, &checks) == nil && checks["started"] != "" {
		if value, ok := checks["runtime_config"]; ok {
			return value
		}
		return "не подключена"
	}
	if probeErr != nil {
		return "admin недоступен"
	}
	return "неверный ответ admin"
}
