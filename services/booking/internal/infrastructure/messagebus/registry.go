package messagebus

import (
	"context"

	"booking/internal/infrastructure/messagebus/subscriber"
	"booking/internal/infrastructure/messagebus/subscriber/scheme/checkin_events"
	"booking/internal/pkg/closer"
	"booking/internal/pkg/inbox"
	"booking/internal/pkg/msgbus/producer"
)

type handlers struct {
	CheckinEvents *checkin_events.MessageHandler
}

type Registry struct {
	BookingEvents *producer.MessageProducer
	handlers      handlers
	subscribers   subscriber.Subscribers
}

func NewRegistry(inbox *inbox.Inbox) *Registry {
	registry := &Registry{
		BookingEvents: producer.NewMessageProducer(),
		subscribers:   subscriber.NewSubscribers(),
		handlers: handlers{
			CheckinEvents: checkin_events.NewMessageHandler(inbox),
		},
	}

	closer.Add(registry.BookingEvents.Close)
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
