package kube

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBookingDependenciesUseCanonicalSQLAndSeparateOwners(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	resources, err := RenderBookingDependencies(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources.Infrastructure) == 0 || len(resources.Migrations) != 2 {
		t.Fatal("incomplete dependency resources")
	}
	var sql corev1.ConfigMap
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(resources.Migrations[0].Object, &sql); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{
		"booking",
		"hotels",
		"review",
		"check-in",
	} {
		paths, err := filepath.Glob(filepath.Join(root, "services", owner, "tools", "migrations", "*.sql"))
		if err != nil || len(paths) == 0 {
			t.Fatalf("canonical SQL not found: %v", err)
		}
		for _, path := range paths {
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if sql.Data[owner+"-"+filepath.Base(path)] != string(body) {
				t.Fatalf("job does not use canonical SQL %s", path)
			}
		}
	}
	var job batchv1.Job
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(resources.Migrations[1].Object, &job); err != nil {
		t.Fatal(err)
	}
	if job.Name != resources.JobName ||
		len(job.Spec.Template.Spec.InitContainers) != 5 ||
		len(job.Spec.Template.Spec.Containers) != 2 ||
		*job.Spec.BackoffLimit != 0 {
		t.Fatalf("incorrect migration/seed sequence: %+v", job.Spec)
	}
	topics := job.Spec.Template.Spec.InitContainers[4]
	if topics.Name != "topics" ||
		!strings.Contains(topics.Args[0], "--topic booking.events.v1") ||
		!strings.Contains(topics.Args[0], "--topic checkin.events.v1") ||
		!strings.Contains(topics.Args[0], "--if-not-exists") {
		t.Fatalf("outbox topics must exist before service rollout: %+v", topics)
	}
	for i, owner := range []string{
		"booking",
		"hotels",
		"review",
		"check-in",
	} {
		container := job.Spec.Template.Spec.InitContainers[i]
		if container.Name != owner ||
			container.Env[0].ValueFrom.SecretKeyRef.Key != "dsn" ||
			container.Env[0].ValueFrom.SecretKeyRef.Name != owner+"-database" ||
			container.Args[1] != "/sql/"+owner ||
			container.Args[len(container.Args)-1] != "up" {
			t.Fatalf("migration crossed database ownership: %+v", container)
		}
	}
	for i, owner := range []string{"hotels", "review"} {
		seed := job.Spec.Template.Spec.Containers[i]
		if seed.Name != owner+"-seed" || seed.Command[0] != "psql" || seed.Env[0].ValueFrom.SecretKeyRef.Key != "dsn" || seed.Env[0].ValueFrom.SecretKeyRef.Name != owner+"-database" {
			t.Fatalf("seed must use its own database: %+v", seed)
		}
		body, err := os.ReadFile(filepath.Join(root, "services", owner, "tools", "seed", "20261007.sql"))
		if err != nil {
			t.Fatal(err)
		}
		if sql.Data[owner+"-seed.sql"] != string(body) {
			t.Fatalf("seed SQL differs for %s", owner)
		}
	}

	cfg, err := config.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, service := range cfg.Services {
		if service.Name != "booking" && service.Name != "hotels" && service.Name != "review" && service.Name != "check-in" {
			continue
		}
		objects, err := Render(root, service, service.Image+":test")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, object := range objects {
			if object.GetKind() != "Deployment" {
				continue
			}
			containers, _, _ := unstructured.NestedSlice(object.Object, "spec", "template", "spec", "containers")
			for _, raw := range containers {
				container := raw.(map[string]any)
				for _, rawEnv := range container["env"].([]any) {
					env := rawEnv.(map[string]any)
					if env["name"] == "KAFKA_BROKERS" && (service.Name == "hotels" || service.Name == "review") {
						t.Fatalf("%s must not depend on Kafka", service.Name)
					}
					if env["name"] == "POSTGRES_DSN" {
						key, _, _ := unstructured.NestedString(env, "valueFrom", "secretKeyRef", "key")
						secret, _, _ := unstructured.NestedString(env, "valueFrom", "secretKeyRef", "name")
						if key != "dsn" || secret != service.Name+"-database" {
							t.Fatalf("%s uses foreign database key %q", service.Name, key)
						}
						found = true
					}
				}
			}
		}
		if !found {
			t.Fatalf("%s missing database configuration", service.Name)
		}
	}
}

func TestMigrationRevisionIsStableAndChangesWithSQL(t *testing.T) {
	root := t.TempDir()
	write := func(relative, body string) {
		t.Helper()
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("deploy/booking-dependencies/kustomization.yaml", "resources:\n  - namespace.yaml\n")
	write(
		"deploy/booking-dependencies/namespace.yaml",
		"apiVersion: v1\nkind: Namespace\nmetadata:\n  name: hotelflow\n",
	)
	for _, owner := range []string{
		"booking",
		"hotels",
		"review",
		"check-in",
	} {
		write("services/"+owner+"/tools/migrations/001_initial.sql", "-- +goose Up\nselect 1;\n")
	}
	write("services/hotels/tools/seed/20261007.sql", "select 1;\n")
	write("services/review/tools/seed/20261007.sql", "select 1;\n")
	first, err := RenderBookingDependencies(root)
	if err != nil {
		t.Fatal(err)
	}
	same, err := RenderBookingDependencies(root)
	if err != nil || first.JobName != same.JobName {
		t.Fatalf("retry must reuse the same completed Job: %v", err)
	}
	write("services/booking/tools/migrations/002_next.sql", "-- +goose Up\nselect 2;\n")
	next, err := RenderBookingDependencies(root)
	if err != nil || next.JobName == first.JobName {
		t.Fatalf("new migration must create a new immutable Job: %v", err)
	}
}

func TestWaitJobReportsFailureWithoutWaitingForTimeout(t *testing.T) {
	for _, test := range []struct {
		condition batchv1.JobConditionType
		wantError bool
	}{
		{
			batchv1.JobComplete,
			false,
		},
		{
			batchv1.JobFailed,
			true,
		},
	} {
		t.Run(string(test.condition), func(t *testing.T) {
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "migration",
					Namespace: "hotelflow",
				},
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{{
						Type:    test.condition,
						Status:  corev1.ConditionTrue,
						Message: "test",
					}},
				},
			}
			client := &Client{
				Typed: fake.NewSimpleClientset(job),
			}
			err := client.WaitJob(context.Background(), job.Namespace, job.Name)
			if (err != nil) != test.wantError {
				t.Fatalf("result: %v", err)
			}
			if err != nil && !strings.Contains(err.Error(), "logs job/migration") {
				t.Fatalf("missing recovery diagnostic: %v", err)
			}
		})
	}
}

func TestSeparatePostgresInstancesAndNotificationWithoutDatabase(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	resources, err := RenderBookingDependencies(root)
	if err != nil {
		t.Fatal(err)
	}
	instances := map[string]bool{}
	volumes := map[string]bool{}
	for _, object := range resources.Infrastructure {
		if object.GetKind() == "Service" && object.GetName() == "postgres" {
			t.Fatal("shared Postgres service still exists")
		}
		if object.GetKind() != "Deployment" || !strings.HasSuffix(object.GetName(), "-postgres") {
			continue
		}
		var deployment struct {
			Spec struct {
				Template struct {
					Spec corev1.PodSpec
				}
			}
		}
		body, err := json.Marshal(object.Object)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &deployment); err != nil {
			t.Fatal(err)
		}
		owner := strings.TrimSuffix(object.GetName(), "-postgres")
		instances[owner] = true
		for _, volume := range deployment.Spec.Template.Spec.Volumes {
			if volume.PersistentVolumeClaim == nil {
				continue
			}
			claim := volume.PersistentVolumeClaim.ClaimName
			if claim != owner+"-db-data" || volumes[claim] {
				t.Fatalf("shared or foreign database volume: %s", claim)
			}
			volumes[claim] = true
		}
		for _, container := range deployment.Spec.Template.Spec.Containers {
			for _, env := range container.Env {
				if env.Name == "POSTGRES_PASSWORD" && env.ValueFrom.SecretKeyRef.Name != owner+"-database" {
					t.Fatal("foreign database credentials")
				}
			}
		}
	}
	if len(instances) != 4 || len(volumes) != 4 {
		t.Fatalf("expected four independent instances and volumes: %v, %v", instances, volumes)
	}
	cfg, err := config.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, service := range cfg.Services {
		if service.Name != "notification" {
			continue
		}
		if !NeedsBookingDependencies([]config.Service{service}) {
			t.Fatal("notification must start Kafka")
		}
		objects, err := Render(root, service, service.Image+":test")
		if err != nil {
			t.Fatal(err)
		}
		kafka := false
		for _, object := range objects {
			if object.GetKind() != "Deployment" {
				continue
			}
			containers, _, _ := unstructured.NestedSlice(object.Object, "spec", "template", "spec", "containers")
			for _, raw := range containers {
				container := raw.(map[string]any)
				for _, rawEnv := range container["env"].([]any) {
					env := rawEnv.(map[string]any)
					if env["name"] == "POSTGRES_DSN" {
						t.Fatal("notification must not receive a database")
					}
					if env["name"] == "KAFKA_BROKERS" && env["value"] == "kafka:9092" {
						kafka = true
					}
				}
			}
		}
		if !kafka {
			t.Fatal("notification is missing Kafka configuration")
		}
	}
}
