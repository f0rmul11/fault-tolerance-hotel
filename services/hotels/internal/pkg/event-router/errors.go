package event_router

import "fmt"

// ErrNoSuchHandler ошибка отсутствия обработчика
type ErrNoSuchHandler[TEvent comparable] struct {
	eventType TEvent
}

func (e *ErrNoSuchHandler[TEvent]) Error() string {
	return fmt.Sprintf(
		"Для типа события %v не зарегистрирован обработчик",
		e.eventType,
	)
}

// NewErrNoSuchHandler конструктор
func NewErrNoSuchHandler[TEvent comparable](eventType TEvent) error {
	return &ErrNoSuchHandler[TEvent]{eventType: eventType}
}
