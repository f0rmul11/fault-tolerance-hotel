package consumer

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/IBM/sarama"
)

type MessageHandler func(context.Context, *sarama.ConsumerMessage, map[string]string) error

type TopicConsumer struct {
	topic   string
	group   sarama.ConsumerGroup
	cancel  context.CancelFunc
	mu      sync.Mutex
	stopped bool
	done    chan struct{}
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
	done := make(chan struct{})
	s.done = done
	s.mu.Unlock()

	go func() {
		for {
			select {
			case err, ok := <-s.group.Errors():
				if !ok {
					return
				}
				slog.ErrorContext(ctx, "Ошибка Kafka consumer", "topic", s.topic, "error", err)
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			err := s.group.Consume(ctx, []string{s.topic}, groupSubscriber{
				messageHandler: handler,
			})
			if ctx.Err() != nil || errors.Is(err, sarama.ErrClosedConsumerGroup) {
				return
			}
			if err != nil {
				slog.ErrorContext(ctx, "Не удалось запустить сессию Kafka", "topic", s.topic, "error", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
			}
		}
	}()
}

func (s *TopicConsumer) Stop() {
	s.mu.Lock()
	s.stopped = true
	if s.cancel != nil {
		s.cancel()
	}
	done := s.done
	s.mu.Unlock()

	if done != nil {
		<-done
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
