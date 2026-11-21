package producer

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"booking/internal/pkg/connector/kafka"
	"booking/internal/pkg/event"
	"booking/internal/pkg/telemetry/traces"
)

type MessageProducer struct {
	producer sarama.SyncProducer
}

func NewMessageProducer() *MessageProducer {
	return &MessageProducer{
		producer: kafka.MustSyncProducer(),
	}
}

func (p *MessageProducer) Close() error {
	return p.producer.Close()
}

// HandlePartition отправляет события в партицию, которую выбрал outbox при записи.
func (p *MessageProducer) HandlePartition(ctx context.Context, partitionID int32, events event.Events) error {
	// Sarama может вернуть частичный успех SendMessages. Следующее событие
	// с тем же ключом передаём только после ACK предыдущего.
	keysInBatch := make(map[string]struct{}, len(events))
	start := 0

	for i, ev := range events {
		key := string(ev.Key)
		if _, exists := keysInBatch[key]; exists {
			if err := p.sendBatch(ctx, partitionID, events[start:i]); err != nil {
				return err
			}

			clear(keysInBatch)
			start = i
		}

		keysInBatch[key] = struct{}{}
	}

	if start == len(events) {
		return nil
	}

	return p.sendBatch(ctx, partitionID, events[start:])
}

func (p *MessageProducer) sendBatch(ctx context.Context, partitionID int32, events event.Events) error {
	messages := make([]*sarama.ProducerMessage, 0, len(events))
	spans := make(map[*sarama.ProducerMessage]trace.Span, len(events))

	for _, ev := range events {
		msg, span := newKafkaMessage(ctx, partitionID, ev)
		defer span.End()

		messages = append(messages, msg)
		spans[msg] = span
	}

	err := p.producer.SendMessages(messages)
	if err == nil {
		return nil
	}

	// Sarama указывает сообщения с ошибками при частичной отправке пачки.
	var failures sarama.ProducerErrors
	if errors.As(err, &failures) {
		for _, failure := range failures {
			if failure == nil {
				continue
			}

			if span, ok := spans[failure.Msg]; ok {
				traces.RecordError(span, failure.Err)
			}
		}
		return err
	}

	for _, span := range spans {
		traces.RecordError(span, err)
	}

	return err
}

// У каждого события пачки свой родительский span. Контекст текущей попытки
// добавляем в копию заголовков, чтобы повторная отправка сохранила исходного родителя.
func newKafkaMessage(ctx context.Context, partitionID int32, ev event.Event) (*sarama.ProducerMessage, trace.Span) {
	headers := make(map[string]string)
	_ = json.Unmarshal(ev.Headers, &headers)
	if headers == nil {
		headers = make(map[string]string)
	}

	parent := traces.Extract(ctx, headers)
	messageCtx, span := traces.Start(parent, "kafka.publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.destination.name", ev.Schema),
			attribute.String("messaging.operation.type", "send"),
			attribute.String("messaging.message.id", headers["x-correlation-id"]),
		),
	)
	traces.Inject(messageCtx, headers)

	records := make([]sarama.RecordHeader, 0, len(headers))
	for key, value := range headers {
		records = append(records, sarama.RecordHeader{
			Key:   []byte(key),
			Value: []byte(value),
		})
	}

	return &sarama.ProducerMessage{
		Topic:     ev.Schema,
		Partition: partitionID,
		Key:       sarama.ByteEncoder(ev.Key),
		Headers:   records,
		Value:     sarama.ByteEncoder(ev.Body),
	}, span
}
