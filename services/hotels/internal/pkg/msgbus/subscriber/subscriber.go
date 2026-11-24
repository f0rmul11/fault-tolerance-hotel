package subscriber

import (
	"context"

	"hotels/internal/pkg/connector/kafka"
	"hotels/internal/pkg/connector/kafka/consumer"
)

type MessageSubscriber struct {
	subscriber *consumer.TopicConsumer
}

func NewMessageSubscriber(topic string) *MessageSubscriber {
	return &MessageSubscriber{
		subscriber: consumer.NewTopicConsumer(topic, kafka.MustConsumerGroup()),
	}
}

func (m *MessageSubscriber) Subscribe(ctx context.Context, handler consumer.MessageHandler) {
	m.subscriber.Subscribe(ctx, handler)
}

func (m *MessageSubscriber) Close() error {
	return m.subscriber.Close()
}

func (m *MessageSubscriber) Stop() {
	m.subscriber.Stop()
}

func (m *MessageSubscriber) Errors() <-chan error {
	return m.subscriber.Errors()
}
