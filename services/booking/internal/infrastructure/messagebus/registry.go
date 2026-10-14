package messagebus

import (
	"context"

	"booking/internal/infrastructure/messagebus/producer"
	"booking/internal/infrastructure/messagebus/subscriber"
	"booking/internal/infrastructure/messagebus/subscriber/scheme/checkin_events"
	"booking/internal/pkg/closer"
	"booking/internal/pkg/inbox"
)

type handlers struct {
	CheckinEvents *checkin_events.MessageHandler
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
		handlers:    handlers{CheckinEvents: checkin_events.NewMessageHandler(inbox)},
	}
	closer.Add(registry.Producers.BookingEvents.Close)
	closer.Add(registry.subscribers.CheckinEvents.Close)
	return registry
}

func (r *Registry) Run(ctx context.Context) {
	r.subscribers.CheckinEvents.Subscribe(ctx, r.handlers.CheckinEvents.Handle)
}

func (r *Registry) Stop() error {
	r.subscribers.CheckinEvents.Stop()
	return nil
}
