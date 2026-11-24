package event_router

import "context"

func chain[TEvent comparable, Event any](interceptors ...EventHandlerInterceptor[TEvent, Event]) EventHandlerInterceptor[TEvent, Event] {
	n := len(interceptors)

	return func(ctx context.Context, event Event, handler Handler[Event]) error {
		chainer := func(currentInterceptor EventHandlerInterceptor[TEvent, Event], currentHandler Handler[Event]) Handler[Event] {
			return func(currentCtx context.Context, currentEvent Event) error {
				return currentInterceptor(currentCtx, currentEvent, currentHandler)
			}
		}

		chainedHandler := handler
		for i := n - 1; i >= 0; i-- {
			chainedHandler = chainer(interceptors[i], chainedHandler)
		}

		return chainedHandler(ctx, event)
	}
}
