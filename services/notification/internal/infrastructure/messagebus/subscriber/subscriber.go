package subscriber

import (
	"errors"
	"fmt"

	"notification/config"
	"notification/internal/pkg/connector/kafka/consumer"
	"notification/internal/pkg/msgbus/subscriber"
	"notification/internal/pkg/msgbus/subscriber/retry"
)

type Subscribers []*subscriber.MessageSubscriber

func NewSubscribers(cfg *config.Config, producer retry.Producer, handler consumer.MessageHandler) (Subscribers, error) {
	sources := map[string]struct {
		group  string
		router *retry.Router
	}{
		config.BookingEventsTopic: {
			group: cfg.ConsumerGroup,
			router: retry.NewMainRouter(
				producer,
				config.BookingRetry1Topic,
				config.BookingDLQTopic,
				cfg.Retry[config.BookingRetry1Topic].Delay,
			),
		},
		config.BookingRetry1Topic: {
			group: cfg.Retry[config.BookingRetry1Topic].Group,
			router: retry.NewRetryRouter(
				producer,
				config.BookingEventsTopic,
				config.BookingRetry2Topic,
				config.BookingDLQTopic,
				cfg.Retry[config.BookingRetry2Topic].Delay,
			),
		},
		config.BookingRetry2Topic: {
			group: cfg.Retry[config.BookingRetry2Topic].Group,
			router: retry.NewTerminalRouter(
				producer,
				config.BookingEventsTopic,
				config.BookingDLQTopic,
			),
		},
	}

	var subscribers Subscribers
	for topic, source := range sources {
		s, err := subscriber.NewMessageSubscriber(cfg.KafkaBrokers, topic, source.group, source.router, handler)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("создать группу %s для %s: %w", source.group, topic, err), subscribers.Close())
		}

		subscribers = append(subscribers, s)
	}

	return subscribers, nil
}

func (s Subscribers) Close() error {
	var err error
	for _, sub := range s {
		err = errors.Join(err, sub.Close())
	}
	return err
}
