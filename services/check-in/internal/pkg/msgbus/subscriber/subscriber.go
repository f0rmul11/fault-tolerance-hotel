package subscriber

import (
	"context"

	"check-in/internal/pkg/connector/kafka"
	"check-in/internal/pkg/connector/kafka/consumer"

	"github.com/IBM/sarama"
)

type MessageSubscriber struct {
	subscriber *consumer.TopicConsumer
}

func NewMessageSubscriber(topic string) *MessageSubscriber {
	return &MessageSubscriber{subscriber: consumer.NewTopicConsumer(topic, kafka.MustConsumerGroup())}
}

func (m *MessageSubscriber) Subscribe(ctx context.Context, handler consumer.MessageHandler) {
	m.subscriber.Subscribe(ctx, m.handle(handler))
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

func (m *MessageSubscriber) handle(handler consumer.MessageHandler) consumer.MessageHandler {
	return func(ctx context.Context, session sarama.ConsumerGroupSession, message *sarama.ConsumerMessage) error {
		return handler(ctx, session, message)
	}
}
