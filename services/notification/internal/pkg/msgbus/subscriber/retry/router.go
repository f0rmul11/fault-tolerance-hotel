package retry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/IBM/sarama"

	"notification/internal/pkg/telemetry/traces"
)

type Producer interface {
	SendMessage(*sarama.ProducerMessage) (partition int32, offset int64, err error)
}

// Router belongs to one subscription. Its destination never comes from the record.
type Router struct {
	producer      Producer
	originalTopic string
	nextTopic     string
	dlqTopic      string
	backoff       time.Duration
	attempt       int
}

func NewMainRouter(producer Producer, retryTopic, dlqTopic string, backoff time.Duration) *Router {
	return &Router{
		producer:  producer,
		nextTopic: retryTopic,
		dlqTopic:  dlqTopic,
		backoff:   backoff,
	}
}

func NewRetryRouter(producer Producer, originalTopic, nextTopic, dlqTopic string, backoff time.Duration) *Router {
	return &Router{
		producer:      producer,
		originalTopic: originalTopic,
		nextTopic:     nextTopic,
		dlqTopic:      dlqTopic,
		backoff:       backoff,
		attempt:       1,
	}
}

func NewTerminalRouter(producer Producer, originalTopic, dlqTopic string) *Router {
	return &Router{
		producer:      producer,
		originalTopic: originalTopic,
		nextTopic:     dlqTopic,
		dlqTopic:      dlqTopic,
		attempt:       2,
	}
}

func (r *Router) Route(ctx context.Context, src *sarama.ConsumerMessage, h map[string]string, started time.Time, cause error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var failure *routingError
	if !errors.As(cause, &failure) {
		failure = &routingError{
			cause: errors.New("unclassified notification processing error"),
			code:  "unclassified",
		}
	}
	if r.attempt == 0 || failure.code == "invalid_retry_metadata" {
		setOrigin(h, src, started)
	}
	traces.Inject(ctx, h)
	out := r.buildMessage(src, h, failure)
	partition, offset, err := r.producer.SendMessage(out)
	if err != nil {
		return fmt.Errorf("публикация в %s: %w", out.Topic, err)
	}
	slog.InfoContext(ctx, "Уведомление перенесено",
		"topic", out.Topic, "partition", partition, "offset", offset,
		"original_topic", h["original-topic"], "original_partition", h["original-partition"],
		"original_offset", h["original-offset"], "stage", h["failed-stage"], "error_code", failure.code,
	)
	return nil
}

func (r *Router) buildMessage(src *sarama.ConsumerMessage, h map[string]string, failure *routingError) *sarama.ProducerMessage {
	now := time.Now().UTC()
	target := r.nextTopic
	count := r.attempt
	delete(h, "deliver-at")
	delete(h, "dlq-reason")
	switch {
	case failure.permanent:
		target = r.dlqTopic
		h["dlq-reason"] = failure.code
	case target == r.dlqTopic:
		h["dlq-reason"] = "retries_exhausted"
	default:
		jitter := time.Duration(rand.Int64N(int64(r.backoff/5) + 1))
		h["deliver-at"] = now.Add(r.backoff + jitter).Format(time.RFC3339Nano)
		count++
	}

	h["last-error-code"] = failure.code
	h["last-error"] = failure.Error()
	h["last-failed-at"] = now.Format(time.RFC3339Nano)
	h["failed-stage"] = "main"
	if r.attempt > 0 {
		h["failed-stage"] = "retry-" + strconv.Itoa(r.attempt)
	}
	h["retry-count"] = strconv.Itoa(count)
	h["max-retries"] = "2"
	h["retry-policy-version"] = "1"
	h["processor-version"] = "notification-v1"

	out := &sarama.ProducerMessage{
		Topic:     target,
		Partition: src.Partition,
		Timestamp: now,
		Headers:   Write(h),
	}
	// Preserve Kafka nulls as well as empty byte strings.
	if src.Key != nil {
		out.Key = sarama.ByteEncoder(src.Key)
	}
	if src.Value != nil {
		out.Value = sarama.ByteEncoder(src.Value)
	}
	return out
}
