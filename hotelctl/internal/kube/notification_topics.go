package kube

import (
	"crypto/sha256"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const businessTopics = "booking.events.v1 checkin.events.v1 review.rating.events.v1"
const notificationTopics = "notification.booking.retry.1.v1 notification.booking.retry.2.v1 notification.booking.dlq.v1"

const createNotificationTopics = `cd /opt/kafka/bin
for topic in ` + notificationTopics + `; do
    case "$topic" in
        *.dlq.v1) retention=1209600000 ;;
        *)        retention=604800000 ;;
    esac
    ./kafka-topics.sh --bootstrap-server kafka:9092 --create --if-not-exists \
        --topic "$topic" --partitions 3 --replication-factor 3 \
        --config min.insync.replicas=2 --config cleanup.policy=delete \
        --config max.message.bytes=2097152 --config "retention.ms=$retention"

    description=$(./kafka-topics.sh --bootstrap-server kafka:9092 --describe --topic "\Q$topic\E")
    fields=$(printf '%s\n' "$description" | sed -n '/PartitionCount:/{s/Configs: //;p;}' | tr '\t,' '\n')
    for expected in 'PartitionCount: 3' 'ReplicationFactor: 3' min.insync.replicas=2 cleanup.policy=delete max.message.bytes=2097152 "retention.ms=$retention"; do
        if ! printf '%s\n' "$fields" | grep -Fxq "$expected"; then
            echo "Топик $topic: ожидается $expected" >&2
            exit 1
        fi
    done
done
`

// Настройки тем версионируются независимо от миграций и seed существующего стенда.
func notificationTopicsJob() *batchv1.Job {
	checksum := sha256.Sum256([]byte(createNotificationTopics))
	backoff := int32(3)
	deadline := int64(300)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("notification-topics-%x", checksum[:6]),
			Namespace: bookingNamespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:          &backoff,
			ActiveDeadlineSeconds: &deadline,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:                corev1.RestartPolicyNever,
					AutomountServiceAccountToken: boolPointer(false),
					Containers: []corev1.Container{{
						Name:    "topics",
						Image:   "apache/kafka:3.9.1",
						Command: []string{"/bin/sh", "-ec", createNotificationTopics},
						Env: []corev1.EnvVar{{
							Name:  "KAFKA_HEAP_OPTS",
							Value: "-Xms128m -Xmx256m",
						}},
					}},
				},
			},
		},
	}
}
