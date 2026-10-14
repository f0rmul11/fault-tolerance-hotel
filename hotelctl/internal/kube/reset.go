package kube

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/project"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const resetSQL = `do $$
declare tables text;
begin
    select string_agg(format('%I.%I', schemaname, tablename), ', ')
    into tables
    from pg_tables
    where schemaname = 'public' and tablename <> 'goose_db_version';
    if tables is null then
        raise exception 'Нет таблиц приложения: сначала выполните hotelctl up';
    end if;
    execute 'truncate table ' || tables || ' restart identity';
end $$;
`

// ResetResources использует текущие seed; схема и история Goose не меняются.
func ResetResources(root, name string) (*corev1.ConfigMap, *batchv1.Job, error) {
	data := map[string]string{}
	var databases []corev1.Container
	for _, owner := range []string{"booking", "check-in", "hotels", "review"} {
		sql := resetSQL
		if owner == "hotels" || owner == "review" {
			seed, err := os.ReadFile(filepath.Join(root, "services", owner, "tools", "seed", "20261007.sql"))
			if err != nil {
				return nil, nil, err
			}
			sql += string(seed)
		}
		data[owner+".sql"] = sql
		databases = append(databases, corev1.Container{
			Name:    owner,
			Image:   "postgres:16.10-alpine",
			Command: []string{"psql"},
			Args:    []string{"$(POSTGRES_DSN)", "-X", "-v", "ON_ERROR_STOP=1", "--single-transaction", "-f", "/sql/" + owner + ".sql"},
			Env:     databaseEnvironment(owner),
			VolumeMounts: []corev1.VolumeMount{{
				Name:      "sql",
				MountPath: "/sql",
				ReadOnly:  true,
			}},
		})
	}
	groups := []string{}
	for _, owner := range []string{"booking", "check-in", "notification"} {
		body, err := os.ReadFile(filepath.Join(root, "services", owner, "config", "config.yaml"))
		if err != nil {
			return nil, nil, err
		}
		var consumer struct {
			Group string `json:"consumer_group"`
		}
		if err := yaml.Unmarshal(body, &consumer); err != nil {
			return nil, nil, err
		}
		if consumer.Group == "" {
			return nil, nil, fmt.Errorf("не задан consumer_group для %s", owner)
		}
		groups = append(groups, consumer.Group)
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: bookingNamespace,
		},
		Data: data,
	}
	backoff := int32(0)
	deadline := int64(300)
	job := &batchv1.Job{
		ObjectMeta: cm.ObjectMeta,
		Spec: batchv1.JobSpec{
			BackoffLimit:          &backoff,
			ActiveDeadlineSeconds: &deadline,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:                corev1.RestartPolicyNever,
					AutomountServiceAccountToken: boolPointer(false),
					InitContainers:               databases,
					Containers: []corev1.Container{{
						Name:    "kafka",
						Image:   "apache/kafka:3.9.1",
						Command: []string{"/bin/sh", "-ec", resetKafka, "reset"},
						Args:    groups,
						Env: []corev1.EnvVar{{
							Name:  "KAFKA_HEAP_OPTS",
							Value: "-Xms128m -Xmx256m",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "sql",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: name},
							},
						},
					}},
				},
			},
		},
	}
	return cm, job, nil
}

const resetKafka = `set -eu
cd /opt/kafka/bin
for topic in booking.events.v1 checkin.events.v1; do
    ./kafka-topics.sh --bootstrap-server kafka:9092 --delete --if-exists --topic "$topic"
done
while :; do
    topics=$(./kafka-topics.sh --bootstrap-server kafka:9092 --list)
    if ! printf '%s\n' "$topics" | grep -Eq '^(booking|checkin)\.events\.v1$'; then
        break
    fi
    sleep 1
done
groups=$(./kafka-consumer-groups.sh --bootstrap-server kafka:9092 --list)
for group in "$@"; do
    if printf '%s\n' "$groups" | grep -Fxq -- "$group"; then
        ./kafka-consumer-groups.sh --bootstrap-server kafka:9092 --delete --group "$group"
    fi
done
groups=$(./kafka-consumer-groups.sh --bootstrap-server kafka:9092 --list)
for group in "$@"; do
    if printf '%s\n' "$groups" | grep -Fxq -- "$group"; then
        echo "Не удалось удалить consumer group: $group" >&2
        exit 1
    fi
done
for topic in booking.events.v1 checkin.events.v1; do
    ./kafka-topics.sh --bootstrap-server kafka:9092 --create --topic "$topic" --partitions 1 --replication-factor 1
done
`

func (c *Client) Replicas(ctx context.Context, services []config.Service) ([]project.ServiceReplicas, error) {
	var result []project.ServiceReplicas
	for _, service := range services {
		d, err := c.Typed.AppsV1().Deployments(service.Namespace).Get(ctx, service.Deployment, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		count := int32(1)
		if d.Spec.Replicas != nil {
			count = *d.Spec.Replicas
		}
		result = append(result, project.ServiceReplicas{
			Service:  service,
			Replicas: count,
		})
	}
	return result, nil
}

func (c *Client) Scale(ctx context.Context, service config.Service, replicas int32) error {
	deployments := c.Typed.AppsV1().Deployments(service.Namespace)
	scale, err := deployments.GetScale(ctx, service.Deployment, metav1.GetOptions{})
	if err != nil {
		return err
	}
	scale.Spec.Replicas = replicas
	_, err = deployments.UpdateScale(ctx, service.Deployment, scale, metav1.UpdateOptions{})
	return err
}

func (c *Client) WaitStopped(ctx context.Context, service config.Service) error {
	d, err := c.Typed.AppsV1().Deployments(service.Namespace).Get(ctx, service.Deployment, metav1.GetOptions{})
	if err != nil {
		return err
	}
	selector, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil {
		return err
	}
	return Poll(ctx, "остановка "+service.Name, func(ctx context.Context) (bool, error) {
		pods, err := c.Typed.CoreV1().Pods(service.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector.String(),
		})
		if err != nil {
			return false, err
		}
		return len(pods.Items) == 0, nil
	})
}

func (c *Client) ResetData(ctx context.Context, cm *corev1.ConfigMap, job *batchv1.Job, out io.Writer) error {
	jobs := c.Typed.BatchV1().Jobs(bookingNamespace)
	existing, err := jobs.Get(ctx, job.Name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err == nil {
		for _, condition := range existing.Status.Conditions {
			if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
				if err := c.DeleteResetJob(ctx, job.Name); err != nil {
					return err
				}
				existing = nil
				break
			}
		}
	}
	if existing == nil || apierrors.IsNotFound(err) {
		maps := c.Typed.CoreV1().ConfigMaps(bookingNamespace)
		stored, err := maps.Get(ctx, cm.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			_, err = maps.Create(ctx, cm, metav1.CreateOptions{})
		} else if err == nil {
			cm.ResourceVersion = stored.ResourceVersion
			_, err = maps.Update(ctx, cm, metav1.UpdateOptions{})
		}
		if err != nil {
			return err
		}
		if _, err := jobs.Create(ctx, job, metav1.CreateOptions{}); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "Сброс данных: job/%s (БД, seed, Kafka и offsets)\n", job.Name)
	return c.WaitJob(ctx, bookingNamespace, job.Name)
}

func (c *Client) DeleteResetJob(ctx context.Context, name string) error {
	foreground := metav1.DeletePropagationForeground
	err := c.Typed.BatchV1().Jobs(bookingNamespace).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return Poll(ctx, "удаление job/"+name, func(ctx context.Context) (bool, error) {
		_, err := c.Typed.BatchV1().Jobs(bookingNamespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
}

func (c *Client) CleanupReset(ctx context.Context, name string) error {
	if err := c.DeleteResetJob(ctx, name); err != nil {
		return err
	}
	err := c.Typed.CoreV1().ConfigMaps(bookingNamespace).Delete(ctx, name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
