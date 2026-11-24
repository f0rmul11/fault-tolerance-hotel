package event_router

import (
	"context"

	"github.com/samber/lo"
)

// EventRouter маршрутизатор событий
type EventRouter[TEvent comparable, Event any] struct {
	handlers             map[TEvent]EventHandler[TEvent, Event]
	interceptors         []EventHandlerInterceptor[TEvent, Event]
	skipUnknownEventType bool
}

// NewEventRouter конструктор
func NewEventRouter[TEvent comparable, Event any](opts ...RouterOption[TEvent, Event]) *EventRouter[TEvent, Event] {
	router := &EventRouter[TEvent, Event]{
		handlers: make(map[TEvent]EventHandler[TEvent, Event]),
	}

	for _, opt := range opts {
		opt(router)
	}

	return router
}

// RegisterAll регистрирует все переданные обработчики
func (r *EventRouter[TEvent, Event]) RegisterAll(handlers ...EventHandler[TEvent, Event]) {
	lo.ForEach(handlers, func(handler EventHandler[TEvent, Event], _ int) {
		r.handlers[handler.EventType()] = handler
	})
}

// HandleEvent обработка ивента
func (r *EventRouter[TEvent, Event]) HandleEvent(ctx context.Context, eventType TEvent, event Event) error {
	handler, ok := r.handlers[eventType]
	if !ok && r.skipUnknownEventType {
		return nil
	}
	// если не найден обработчик, то возвращаем ошибку
	if !ok {
		return NewErrNoSuchHandler(eventType)
	}

	return chain(r.interceptors...)(ctx, event, handler.HandleEvent)
}
