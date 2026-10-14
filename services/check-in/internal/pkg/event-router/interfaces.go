package event_router

import "context"

// Handler ...
type Handler[Event any] func(ctx context.Context, event Event) error

// EventHandlerInterceptor перехватчик для обработчика событий
type EventHandlerInterceptor[TEvent comparable, Event any] func(ctx context.Context, event Event, handler Handler[Event]) error

// EventHandler events handler
type EventHandler[TEvent, Event any] interface {
	// HandleEvent обработчик события
	HandleEvent(ctx context.Context, event Event) error
	// EventType возвращает тип ивента
	EventType() TEvent
}
