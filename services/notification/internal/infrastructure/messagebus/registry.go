package messagebus

import (
	"context"
	"errors"
	"fmt"

	"notification/config"
	"notification/internal/dto/bookingevent"
	"notification/internal/infrastructure/messagebus/subscriber"
	"notification/internal/infrastructure/messagebus/subscriber/scheme/booking_events"
	"notification/internal/pkg/connector/kafka"
	event_router "notification/internal/pkg/event-router"

	"github.com/IBM/sarama"
)

type Registry struct {
	producer    sarama.SyncProducer
	subscribers subscriber.Subscribers
}

func NewRegistry(cfg *config.Config, router *event_router.EventRouter[string, bookingevent.Event]) (*Registry, error) {
	producer, err := kafka.NewProducer(cfg.KafkaBrokers)
	if err != nil {
		return nil, fmt.Errorf("создать Kafka producer для Notification: %w", err)
	}
	handler := booking_events.NewMessageHandler(router)
	subscribers, err := subscriber.NewSubscribers(cfg, producer, handler.Handle)
	if err != nil {
		return nil, errors.Join(err, producer.Close())
	}

	return &Registry{
		producer:    producer,
		subscribers: subscribers,
	}, nil
}

func (r *Registry) Run(ctx context.Context) {
	for _, sub := range r.subscribers {
		sub.Subscribe(ctx)
	}
}

func (r *Registry) Stop() error {
	// Сначала освобождаем claims, чтобы ни один handler не писал в закрытый producer.
	err := r.subscribers.Close()
	return errors.Join(err, r.producer.Close())
}
