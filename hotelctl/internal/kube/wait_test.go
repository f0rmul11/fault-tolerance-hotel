package kube

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRolloutWaitsForTerminatingOldPod(t *testing.T) {
	now := metav1.Now()
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "gateway",
			Namespace:  "hotel",
			Generation: 2,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "gateway"}},
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 2,
			UpdatedReplicas:    1,
			AvailableReplicas:  1,
			Replicas:           1,
		},
	}
	old := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:              "old",
		Namespace:         "hotel",
		Labels:            map[string]string{"app": "gateway"},
		DeletionTimestamp: &now,
	}}
	unrelated := old.DeepCopy()
	unrelated.Name = "unrelated"
	unrelated.Labels = map[string]string{"app": "booking"}
	client := &Client{Typed: fake.NewSimpleClientset(deployment, old, unrelated)}
	service := config.Service{
		Name:       "gateway",
		Namespace:  "hotel",
		Deployment: "gateway",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := client.WaitDeployment(ctx, service); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("rollout completed while old Pod was terminating: %v", err)
	}
	if err := client.Typed.CoreV1().Pods("hotel").Delete(context.Background(), "old", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	ready, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := client.WaitDeployment(ready, service); err != nil {
		t.Fatalf("finished rollout still blocked by unrelated Pod: %v", err)
	}
}
