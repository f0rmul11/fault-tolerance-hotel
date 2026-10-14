package event_router

// RouterOption опции для роутера
type RouterOption[TEvent comparable, Event any] func(router *EventRouter[TEvent, Event]) *EventRouter[TEvent, Event]

// WithInterceptor добавляет вызов interceptor обертки над каждым обработчиком
func WithInterceptor[TEvent comparable, Event any](interceptor EventHandlerInterceptor[TEvent, Event]) RouterOption[TEvent, Event] {
	return func(router *EventRouter[TEvent, Event]) *EventRouter[TEvent, Event] {
		router.interceptors = append(router.interceptors, interceptor)
		return router
	}
}

// WithSkipUnknownEventType гарантирует скип ошибки при отсутствии обработчика для события
func WithSkipUnknownEventType[TEvent comparable, Event any]() RouterOption[TEvent, Event] {
	return func(router *EventRouter[TEvent, Event]) *EventRouter[TEvent, Event] {
		router.skipUnknownEventType = true
		return router
	}
}
