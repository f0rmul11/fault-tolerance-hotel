package producer

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"check-in/internal/pkg/connector/kafka"
	"check-in/internal/pkg/event"
	"check-in/internal/pkg/telemetry/traces"
)

type MessageProducer struct {
	producer sarama.SyncProducer
}

func NewMessageProducer() *MessageProducer {
	return &MessageProducer{
		producer: kafka.MustSyncProducer(),
	}
}

func (m *MessageProducer) Close() error {
	return m.producer.Close()
}

func (m *MessageProducer) Handle(ctx context.Context, events event.Events) error {
	messages := make([]*sarama.ProducerMessage, 0, len(events))
	spans := make(map[*sarama.ProducerMessage]trace.Span, len(events))
	for _, event := range events {
		// Each outbox event retains its own parent, including in a mixed batch.
		headers := make(map[string]string)
		_ = json.Unmarshal(event.Headers, &headers)
		if headers == nil {
			headers = make(map[string]string)
		}
		parent := traces.Extract(ctx, headers)
		messageCtx, span := traces.Start(parent, "kafka.publish",
			trace.WithSpanKind(trace.SpanKindProducer),
			trace.WithAttributes(
				attribute.String("messaging.system", "kafka"),
				attribute.String("messaging.destination.name", event.Schema),
				attribute.String("messaging.operation.type", "send"),
				attribute.String("messaging.message.id", headers["x-correlation-id"]),
			),
		)
		defer span.End()

		// Mutate only this decoded copy; the stored outbox parent survives retries.
		traces.Inject(messageCtx, headers)
		records := make([]sarama.RecordHeader, 0, len(headers))
		for key, value := range headers {
			records = append(records, sarama.RecordHeader{
				Key:   []byte(key),
				Value: []byte(value),
			})
		}
		message := &sarama.ProducerMessage{
			Topic:   event.Schema,
			Key:     sarama.StringEncoder(event.Key),
			Headers: records,
			Value:   sarama.ByteEncoder(event.Body),
		}
		messages = append(messages, message)
		spans[message] = span
	}

	err := m.producer.SendMessages(messages)
	if err == nil {
		return nil
	}
	var failures sarama.ProducerErrors
	if errors.As(err, &failures) {
		for _, failure := range failures {
			if failure != nil {
				if span, ok := spans[failure.Msg]; ok {
					traces.RecordError(span, failure.Err)
				}
			}
		}
		return err
	}
	for _, span := range spans {
		traces.RecordError(span, err)
	}
	return err
}
