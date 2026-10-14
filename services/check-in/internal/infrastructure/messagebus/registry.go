package messagebus

import (
	"context"

	"check-in/internal/infrastructure/messagebus/producer"
	"check-in/internal/infrastructure/messagebus/subscriber"
	"check-in/internal/infrastructure/messagebus/subscriber/scheme/booking_events"
	"check-in/internal/pkg/closer"
	"check-in/internal/pkg/inbox"
)

type handlers struct {
	BookingEvents *booking_events.MessageHandler
}

type Registry struct {
	Producers   producer.Producers
	handlers    handlers
	subscribers subscriber.Subscribers
}

func NewRegistry(inbox *inbox.Inbox) *Registry {
	registry := &Registry{
		Producers:   producer.NewProducers(),
		subscribers: subscriber.NewSubscribers(),
		handlers:    handlers{BookingEvents: booking_events.NewMessageHandler(inbox)},
	}
	closer.Add(registry.Producers.CheckinEvents.Close)
	closer.Add(registry.subscribers.BookingEvents.Close)
	return registry
}

func (r *Registry) Run(ctx context.Context) {
	r.subscribers.BookingEvents.Subscribe(ctx, r.handlers.BookingEvents.Handle)
}

func (r *Registry) Stop() error {
	r.subscribers.BookingEvents.Stop()
	return nil
}
