package kube

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
)

const bookingNamespace = "hotelflow"
const gooseImage = "ghcr.io/kukymbr/goose-docker:3.26.0@sha256:0cd025636df126e7f66472861ca4db3683bc649be46cd1f6ef1a316209058e23"
const migrationJobRevision = "v9-separate-postgres-public"

type BookingResources struct {
	Infrastructure []*unstructured.Unstructured
	Migrations     []*unstructured.Unstructured
	JobName        string
}

func NeedsBookingDependencies(services []config.Service) bool {
	for _, service := range services {
		if service.Name == "booking" || (service.Name == "hotels" || service.Name == "review") || service.Name == "check-in" || service.Name == "api-gateway" || service.Name == "notification" {
			return true
		}
	}

	return false
}

// RenderBookingDependencies mounts canonical service SQL without maintaining a second schema copy.
func RenderBookingDependencies(root string) (BookingResources, error) {
	infrastructure, err := Render(root, config.Service{
		Name:      "booking-dependencies",
		Manifests: "deploy/booking-dependencies",
	}, "")
	if err != nil {
		return BookingResources{}, err
	}

	data := map[string]string{}
	var items []corev1.KeyToPath
	for _, owner := range []string{
		"booking",
		"hotels",
		"review",
		"check-in",
	} {
		directory := filepath.Join(root, "services", owner, "tools", "migrations")
		entries, err := os.ReadDir(directory)
		if err != nil {
			return BookingResources{}, err
		}

		count := 0
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
				continue
			}

			body, err := os.ReadFile(filepath.Join(directory, entry.Name()))
			if err != nil {
				return BookingResources{}, err
			}

			key := owner + "-" + entry.Name()
			data[key] = string(body)
			items = append(items, corev1.KeyToPath{
				Key:  key,
				Path: owner + "/" + entry.Name(),
			})
			count++
		}

		if count == 0 {
			return BookingResources{}, fmt.Errorf("для %s не найдены SQL-миграции", owner)
		}
	}

	for _, owner := range []string{"hotels", "review"} {
		seed, err := os.ReadFile(filepath.Join(root, "services", owner, "tools", "seed", "20261007.sql"))
		if err != nil {
			return BookingResources{}, err
		}
		key := owner + "-seed.sql"
		data[key] = string(seed)
		items = append(items, corev1.KeyToPath{
			Key:  key,
			Path: key,
		})
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return BookingResources{}, err
	}

	checksum := sha256.Sum256(append(encoded, gooseImage+migrationJobRevision...))
	name := fmt.Sprintf("booking-migrations-%x", checksum[:6])
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: bookingNamespace,
		},
		Data: data,
	}

	backoff := int32(0)
	deadline := int64(300)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: bookingNamespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:          &backoff,
			ActiveDeadlineSeconds: &deadline,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:                corev1.RestartPolicyNever,
					AutomountServiceAccountToken: boolPointer(false),
					InitContainers: []corev1.Container{
						migrationContainer("booking"),
						migrationContainer("hotels"),
						migrationContainer("review"),
						migrationContainer("check-in"),
						{
							Name:  "topics",
							Image: "apache/kafka:3.9.1",
							Command: []string{
								"/bin/sh",
								"-ec",
							},
							Args: []string{`/opt/kafka/bin/kafka-topics.sh --bootstrap-server kafka:9092 --create --if-not-exists --topic booking.events.v1 --partitions 1 --replication-factor 1
/opt/kafka/bin/kafka-topics.sh --bootstrap-server kafka:9092 --create --if-not-exists --topic checkin.events.v1 --partitions 1 --replication-factor 1`},
							Env: []corev1.EnvVar{{
								Name:  "KAFKA_HEAP_OPTS",
								Value: "-Xms128m -Xmx256m",
							}},
						},
					},
					Containers: []corev1.Container{seedContainer("hotels"), seedContainer("review")},
					Volumes: []corev1.Volume{{
						Name: "sql",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: name,
								},
								Items: items,
							},
						},
					}},
				},
			},
		},
	}

	var migrations []*unstructured.Unstructured
	for _, object := range []runtime.Object{
		configMap,
		job,
	} {
		value, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object)
		if err != nil {
			return BookingResources{}, err
		}

		converted := &unstructured.Unstructured{
			Object: value,
		}

		switch object.(type) {
		case *corev1.ConfigMap:
			converted.SetAPIVersion("v1")
			converted.SetKind("ConfigMap")
		case *batchv1.Job:
			converted.SetAPIVersion("batch/v1")
			converted.SetKind("Job")
		}
		migrations = append(migrations, converted)
	}

	return BookingResources{
		Infrastructure: infrastructure,
		Migrations:     migrations,
		JobName:        name,
	}, nil
}

func migrationContainer(owner string) corev1.Container {
	return corev1.Container{
		Name:    owner,
		Image:   gooseImage,
		Command: []string{"/bin/goose"},
		Args: []string{
			"-dir",
			"/sql/" + owner,
			"postgres",
			"$(POSTGRES_DSN)",
			"up",
		},
		Env: databaseEnvironment(owner),
		VolumeMounts: []corev1.VolumeMount{{
			Name:      "sql",
			MountPath: "/sql",
			ReadOnly:  true,
		}},
	}
}

func databaseEnvironment(owner string) []corev1.EnvVar {
	return []corev1.EnvVar{{
		Name: "POSTGRES_DSN",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: owner + "-database",
				},
				Key: "dsn",
			},
		},
	}}
}

func boolPointer(value bool) *bool {
	return &value
}

func (c *Client) WaitJob(ctx context.Context, namespace, name string) error {
	err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		job, err := c.Typed.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		for _, condition := range job.Status.Conditions {
			if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
				return false, fmt.Errorf(
					"задача завершилась ошибкой: %s; проверьте logs job/%s -n %s",
					condition.Message,
					name,
					namespace,
				)
			}

			if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
				return true, nil
			}
		}

		return false, nil
	})
	if err != nil {
		return fmt.Errorf("ожидание задачи %s: %w", name, err)
	}

	return nil
}

// Each service receives only its own database credentials.
func configureBookingRuntime(object *unstructured.Unstructured, service config.Service) {
	if object.GetKind() != "Deployment" || (service.Name != "booking" && service.Name != "hotels" && service.Name != "review" && service.Name != "check-in" && service.Name != "notification") {
		return
	}

	containers, found, _ := unstructured.NestedSlice(object.Object, "spec", "template", "spec", "containers")
	if !found {
		return
	}

	for _, raw := range containers {
		container, ok := raw.(map[string]any)
		if !ok || container["name"] != service.Name {
			continue
		}

		env, _ := container["env"].([]any)
		// Explicit manifest overrides win. Standard manifests need just these defaults.
		names := map[string]bool{}
		for _, item := range env {
			if entry, ok := item.(map[string]any); ok {
				name, _ := entry["name"].(string)
				names[name] = true
			}
		}

		if service.Name != "notification" && !names["POSTGRES_DSN"] {
			env = append(env, map[string]any{
				"name": "POSTGRES_DSN",
				"valueFrom": map[string]any{
					"secretKeyRef": map[string]any{
						"name": service.Name + "-database",
						"key":  "dsn",
					},
				},
			})
		}

		if (service.Name == "booking" || service.Name == "check-in" || service.Name == "notification") && !names["KAFKA_BROKERS"] {
			env = append(env, map[string]any{
				"name":  "KAFKA_BROKERS",
				"value": "kafka:9092",
			})
		}
		container["env"] = env
	}
	_ = unstructured.SetNestedSlice(object.Object, containers, "spec", "template", "spec", "containers")
}

func seedContainer(owner string) corev1.Container {
	return corev1.Container{
		Name:    owner + "-seed",
		Image:   "postgres:16.10-alpine",
		Command: []string{"psql"},
		Args:    []string{"$(POSTGRES_DSN)", "-v", "ON_ERROR_STOP=1", "-f", "/sql/" + owner + "-seed.sql"},
		Env:     databaseEnvironment(owner),
		VolumeMounts: []corev1.VolumeMount{{
			Name:      "sql",
			MountPath: "/sql",
			ReadOnly:  true,
		}},
	}
}
