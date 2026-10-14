package messagebus

import (
	"context"

	"notification/internal/infrastructure/messagebus/subscriber"
	"notification/internal/infrastructure/messagebus/subscriber/scheme/booking_events"
	"notification/internal/pkg/closer"
	event_router "notification/internal/pkg/event-router"
)

type Registry struct {
	bookingEvents *booking_events.MessageHandler
	subscribers   subscriber.Subscribers
}

func NewRegistry(router *event_router.EventRouter[string, []byte]) *Registry {
	registry := &Registry{
		bookingEvents: booking_events.NewMessageHandler(router),
		subscribers:   subscriber.NewSubscribers(),
	}
	closer.Add(registry.subscribers.BookingEvents.Close)
	return registry
}

func (r *Registry) Run(ctx context.Context) {
	r.subscribers.BookingEvents.Subscribe(ctx, r.bookingEvents.Handle)
}

func (r *Registry) Stop() error {
	r.subscribers.BookingEvents.Stop()
	return nil
}
