package stand

import (
	"context"
	"testing"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/kube"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFinalPromotionRequiresLastUnabortedPause(t *testing.T) {
	for _, test := range []struct {
		name      string
		index     int64
		aborted   bool
		paused    bool
		wantError bool
	}{
		{name: "final", index: 2, paused: true},
		{name: "earlier", index: 0, paused: true, wantError: true},
		{name: "aborted", index: 2, paused: true, aborted: true, wantError: true},
		{name: "still progressing", index: 1, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			status := map[string]any{"currentStepIndex": test.index, "abort": test.aborted}
			if test.paused {
				status["pauseConditions"] = []any{map[string]any{"reason": "CanaryPauseStep"}}
			}
			r := &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{"strategy": map[string]any{"canary": map[string]any{
					"steps": []any{map[string]any{"pause": map[string]any{}}, map[string]any{"setWeight": int64(100)}, map[string]any{"pause": map[string]any{}}},
				}}},
				"status": status,
			}}
			if err := requireFinalPause(r); (err != nil) != test.wantError {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestHotelsMeshAcceptsNativeSidecarAndRejectsUnready(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, ready := range []bool{false, true} {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Name: "hotels-1", Namespace: "hotelflow", Labels: map[string]string{"app.kubernetes.io/name": "hotels"},
			}}
			statuses := []corev1.ContainerStatus{{Name: "istio-proxy", Ready: ready}}
			if native {
				pod.Status.InitContainerStatuses = statuses
			} else {
				pod.Status.ContainerStatuses = statuses
			}
			c := &kube.Client{Typed: fake.NewClientset(pod)}
			if err := requireHotelsSidecars(context.Background(), c); (err == nil) != ready {
				t.Fatalf("native=%t ready=%t err=%v", native, ready, err)
			}
		}
	}
}
