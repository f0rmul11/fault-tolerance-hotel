package kube

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestResetUsesConfiguredConsumerGroups(t *testing.T) {
	root := t.TempDir()
	for _, owner := range []string{"booking", "check-in", "notification", "hotels"} {
		dir := filepath.Join(root, "services", owner, "config")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("consumer_group: "+owner+"-test\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := resetConsumerGroups(root); err == nil {
		t.Fatal("reset accepted a configuration without retry groups")
	}

	path := filepath.Join(root, "services", "notification", "config", "config.yaml")
	explicit := `consumer_group: main
retry:
  notification.booking.retry.2.v1:
    group: long-delay
    delay: 40s
  notification.booking.retry.1.v1:
    group: short-delay
    delay: 3s
`
	if err := os.WriteFile(path, []byte(explicit), 0o600); err != nil {
		t.Fatal(err)
	}
	groups, err := resetConsumerGroups(root)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(groups)
	if !slices.Equal(groups, []string{"booking-test", "check-in-test", "hotels-test", "long-delay", "main", "short-delay"}) {
		t.Fatalf("ignored explicit consumer groups: %v", groups)
	}
}

func TestResetJobResumesOrReplacesFailedAttempt(t *testing.T) {
	for _, failed := range []bool{false, true} {
		name := "data-reset-test"
		condition := batchv1.JobComplete
		if failed {
			condition = batchv1.JobFailed
		}
		old := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: bookingNamespace,
			},
			Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{{
					Type:   condition,
					Status: corev1.ConditionTrue,
				}},
			},
		}
		client := fake.NewSimpleClientset(old)
		created := 0
		client.PrependReactor("create", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
			created++
			job := action.(ktesting.CreateAction).GetObject().(*batchv1.Job)
			job.Status.Conditions = []batchv1.JobCondition{{
				Type:   batchv1.JobComplete,
				Status: corev1.ConditionTrue,
			}}
			return false, nil, nil
		})
		root, err := filepath.Abs("../../..")
		if err != nil {
			t.Fatal(err)
		}
		cm, job, err := ResetResources(root, name)
		if err != nil {
			t.Fatal(err)
		}
		kafka := job.Spec.Template.Spec.Containers[0]
		for _, group := range []string{"notification-booking-events", "notification-booking-events-retry-1", "notification-booking-events-retry-2"} {
			if !slices.Contains(kafka.Args, group) {
				t.Fatalf("reset не удаляет offsets группы %s", group)
			}
		}
		if !strings.HasSuffix(kafka.Command[2], createNotificationTopics) {
			t.Fatal("после reset параметры retry и DLQ должны совпадать с параметрами при создании стенда")
		}
		cluster := &Client{Typed: client}
		if err := cluster.ResetData(t.Context(), cm, job, io.Discard); err != nil {
			t.Fatal(err)
		}
		if (created == 1) != failed {
			t.Fatalf("failed=%t, создано новых Job=%d", failed, created)
		}
		if !strings.Contains(cm.Data["hotels.sql"], "insert into public.hotels") || !strings.Contains(cm.Data["review.sql"], "insert into public.ratings") {
			t.Fatal("сброс должен загрузить канонические seed после очистки")
		}
		if err := cluster.CleanupReset(t.Context(), name); err != nil {
			t.Fatal(err)
		}
	}
}
