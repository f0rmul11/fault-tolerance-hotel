package messagebus

import (
	"context"

	"hotels/internal/infrastructure/messagebus/subscriber"
	"hotels/internal/infrastructure/messagebus/subscriber/scheme/review_events"
	"hotels/internal/pkg/closer"
	event_router "hotels/internal/pkg/event-router"
)

type handlers struct {
	ReviewEvents *review_events.MessageHandler
}

type Registry struct {
	handlers    handlers
	subscribers subscriber.Subscribers
}

func NewRegistry(router *event_router.EventRouter[string, []byte]) *Registry {
	registry := &Registry{
		subscribers: subscriber.NewSubscribers(),
		handlers: handlers{
			ReviewEvents: review_events.NewMessageHandler(router),
		},
	}
	closer.Add(registry.subscribers.ReviewEvents.Close)
	return registry
}

func (r *Registry) Run(ctx context.Context) {
	r.subscribers.ReviewEvents.Subscribe(ctx, r.handlers.ReviewEvents.Handle)
}

func (r *Registry) Stop() error {
	r.subscribers.ReviewEvents.Stop()
	return nil
}
