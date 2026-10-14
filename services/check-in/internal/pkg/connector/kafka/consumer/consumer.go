package consumer

import (
	"context"
	"log/slog"
	"sync"

	"github.com/IBM/sarama"
)

type MessageHandler func(context.Context, sarama.ConsumerGroupSession, *sarama.ConsumerMessage) error

type TopicConsumer struct {
	topic   string
	group   sarama.ConsumerGroup
	cancel  context.CancelFunc
	mu      sync.Mutex
	stopped bool
}

func NewTopicConsumer(topic string, group sarama.ConsumerGroup) *TopicConsumer {
	return &TopicConsumer{
		topic: topic,
		group: group,
	}
}

func (s *TopicConsumer) Subscribe(ctx context.Context, handler MessageHandler) {
	s.mu.Lock()
	if s.stopped || s.cancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.mu.Unlock()
	go func() {
		for {
			if ctx.Err() != nil {
				break
			}
			// TODO: Add setup & cleanup
			err := s.group.Consume(ctx, []string{s.topic}, groupSubscriber{
				messageHandler: handler,
			})
			if err != nil {
				slog.Error(err.Error())
			}
		}
	}()
}

func (s *TopicConsumer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *TopicConsumer) Close() error {
	s.Stop()
	if s.group == nil {
		return nil
	}
	return s.group.Close()
}

func (s *TopicConsumer) Errors() <-chan error {
	return s.group.Errors()
}
