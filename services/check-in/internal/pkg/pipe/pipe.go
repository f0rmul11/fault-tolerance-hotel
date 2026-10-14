package pipe

import (
	"context"
)

// Func шаблон функции
type Func[T any] func(ctx context.Context, value T) (T, error)

// AnywayFunc шаблон функции
type AnywayFunc[T any] func(ctx context.Context, value T, err error)

// OverFunc шаблон функци
type OverFunc[T any] func(ctx context.Context, value T, pipe Pipe[T]) (T, error)

// Pipe набор функций, которые должны выполнится для некоторой сущности
type Pipe[T any] []Func[T]

// With начинается конфигурации последовательности для некоторого значения
func With[T any](with Func[T]) Pipe[T] {
	return Pipe[T]{with}
}

// Over начинается конфигурации последовательности для некоторого значения
func Over[T any](over OverFunc[T], p Pipe[T]) Pipe[T] {
	return Pipe[T]{func(ctx context.Context, value T) (T, error) {
		return over(ctx, value, p)
	}}
}

// With добавляет к последовательности новое звено
func (pipe Pipe[T]) With(with Func[T]) Pipe[T] {
	return append(pipe, with)
}

// Over добавляет к последовательности новое звено, обобщенное общей логикой
func (pipe Pipe[T]) Over(f OverFunc[T], p Pipe[T]) Pipe[T] {
	return append(pipe, func(ctx context.Context, value T) (T, error) {
		return f(ctx, value, p)
	})
}

// Run запускает выполнение всех последовательностей возвращая финальный результат
func (pipe Pipe[T]) Run(ctx context.Context, value T) Observer[T] {
	var err error
	for _, with := range pipe {
		value, err = with(ctx, value)
		if err != nil {
			return Observer[T]{Result[T]{
				res: value,
				err: err,
			}}
		}
	}

	return Observer[T]{Result[T]{
		res: value,
		err: err,
	}}
}

// Result результат выполнения пайплайна
type Result[T any] struct {
	res T
	err error
}

// Get получение ответа
func (r Result[T]) Get() (T, error) {
	return r.res, r.err
}

// Err ошибка выполнения
func (r Result[T]) Err() error {
	return r.err
}

// Observer ответ от пайпа
type Observer[T any] struct {
	Result[T]
}
