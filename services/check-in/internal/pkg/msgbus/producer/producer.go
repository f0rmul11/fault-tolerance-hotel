package producer

import (
	"context"
	"encoding/json"

	"check-in/internal/pkg/connector/kafka"
	"check-in/internal/pkg/event"

	"github.com/IBM/sarama"
	"github.com/samber/lo"
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

func (m *MessageProducer) Handle(_ context.Context, events event.Events) error {
	return m.producer.SendMessages(lo.Map(events, func(msg event.Event, _ int) *sarama.ProducerMessage {
		return &sarama.ProducerMessage{
			Topic:   msg.Schema,
			Key:     sarama.StringEncoder(msg.Key),
			Headers: m.parseHeaders(msg.Headers),
			Value:   sarama.ByteEncoder(msg.Body),
		}
	}))
}

func (m *MessageProducer) parseHeaders(headers event.Raw) []sarama.RecordHeader {
	mapped := make(map[string]string)
	if err := json.Unmarshal(headers, &mapped); err != nil {
		return nil
	}

	var records = make([]sarama.RecordHeader, 0, len(mapped))

	for key, value := range mapped {
		records = append(records, sarama.RecordHeader{
			Key:   []byte(key),
			Value: []byte(value),
		})
	}
	return records
}
