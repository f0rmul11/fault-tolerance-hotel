package subscriber

import (
	"context"
	"time"

	"github.com/IBM/sarama"

	"notification/internal/pkg/connector/kafka"
	"notification/internal/pkg/connector/kafka/consumer"
	"notification/internal/pkg/msgbus/subscriber/retry"
)

type MessageSubscriber struct {
	subscriber *consumer.TopicConsumer
	router     *retry.Router
	handler    consumer.MessageHandler
}

func NewMessageSubscriber(brokers []string, topic, groupName string, router *retry.Router, handler consumer.MessageHandler) (*MessageSubscriber, error) {
	group, err := kafka.NewConsumerGroup(brokers, groupName)
	if err != nil {
		return nil, err
	}
	router.InitTopicMetrics(topic)
	return &MessageSubscriber{
		subscriber: consumer.NewTopicConsumer(topic, group),
		router:     router,
		handler:    handler,
	}, nil
}

func (m *MessageSubscriber) Subscribe(ctx context.Context) {
	m.subscriber.Subscribe(ctx, m.Handle)
}

func (m *MessageSubscriber) Handle(ctx context.Context, message *sarama.ConsumerMessage, headers map[string]string) error {
	started := time.Now().UTC()
	due, err := m.router.DeliverAt(headers, message, started)
	if err != nil {
		return m.router.Route(ctx, message, headers, started, retry.DLQErr("invalid_retry_metadata", err))
	}
	for delay := time.Until(due); delay > 0; delay = time.Until(due) {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	err = m.handler(ctx, message, headers)
	// трекаем метрику
	m.router.ObserveAttempt(message.Topic, err)

	if err != nil {
		return m.router.Route(ctx, message, headers, started, err)
	}
	return nil
}

func (m *MessageSubscriber) Close() error {
	return m.subscriber.Close()
}
