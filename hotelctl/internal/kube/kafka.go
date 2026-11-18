package kube

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// KafkaOffsets читает конец партиций по прямому listener, вне инъекции producer.
func (c *Client) KafkaOffsets(ctx context.Context, topic string) (map[int]int64, error) {
	pods, err := c.Typed.CoreV1().Pods("hotelflow").List(ctx, metav1.ListOptions{
		LabelSelector: "kafka-cluster=hotelflow",
	})
	if err != nil {
		return nil, err
	}
	name := ""
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				name = pod.Name
				break
			}
		}
		if name != "" {
			break
		}
	}
	if name == "" {
		return nil, fmt.Errorf("нет готового брокера Kafka")
	}
	request := c.Typed.CoreV1().RESTClient().Post().Namespace("hotelflow").
		Resource("pods").Name(name).SubResource("exec").VersionedParams(&corev1.PodExecOptions{
		Container: "kafka",
		Command: []string{
			"/opt/kafka/bin/kafka-get-offsets.sh",
			"--bootstrap-server", "kafka:9092", "--topic", topic, "--time", "latest",
		},
		Stdout: true,
		Stderr: true,
	}, scheme.ParameterCodec)
	cfg := rest.CopyConfig(c.REST)
	cfg.Timeout = 0
	executor, err := remotecommand.NewSPDYExecutor(cfg, http.MethodPost, request.URL())
	if err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr}); err != nil {
		return nil, fmt.Errorf("чтение offsets: %w: %s", err, stderr.String())
	}
	result := make(map[int]int64)
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		parts := strings.Split(line, ":")
		if len(parts) != 3 || parts[0] != topic {
			return nil, fmt.Errorf("неожиданный ответ offsets: %q", line)
		}
		partition, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, err
		}
		offset, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return nil, err
		}
		result[partition] = offset
	}
	return result, nil
}
