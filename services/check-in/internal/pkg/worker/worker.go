package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type Worker interface {
	Start(ctx context.Context) error
	Stop() error
}

type settings struct {
	// интервал запуска джобы
	interval func(context.Context) time.Duration
	// максимально возможное количество конкурирующих джоб (запускаются если предыдующие джобы неуспели закончить выполнение)
	concurrentTasksCount func(context.Context) int
}

func NewWorker(
	ctx context.Context,
	task func(ctx context.Context),
	interval func(context.Context) time.Duration,
	concurrentTasksCount func(context.Context) int,
) Worker {
	return &worker{
		ctx:  ctx,
		task: task,
		settings: &settings{
			interval:             interval,
			concurrentTasksCount: concurrentTasksCount,
		},
	}
}

type worker struct {
	settings *settings

	ctx          context.Context
	task         func(ctx context.Context)
	interval     time.Duration
	concurrentCh chan struct{} // Канал для ограничения количества параллельных задач.
	loopDone     chan struct{}
	stopCh       chan struct{}
	stopChMutex  sync.Mutex
	taskWg       sync.WaitGroup // WaitGroup для отслеживания количества запущенных задач.
}

func (w *worker) Start(ctx context.Context) error {
	w.stopChMutex.Lock()
	defer w.stopChMutex.Unlock()
	if w.stopCh != nil {
		return nil
	}

	w.concurrentCh = make(chan struct{}, w.settings.concurrentTasksCount(ctx))
	w.stopCh = make(chan struct{})
	w.loopDone = make(chan struct{})
	stopCh := w.stopCh
	loopDone := w.loopDone

	go func() {
		defer close(loopDone)
		for {
			select {
			case <-w.ctx.Done():
				return // Context cancelled, stop the worker.
			case <-stopCh:
				return // Worker was stopped.
			case <-time.After(w.settings.interval(ctx)):
				if len(w.concurrentCh) < cap(w.concurrentCh) { // Если место есть в канале, запускаем еще одну задачу.
					w.concurrentCh <- struct{}{}
					w.taskWg.Add(1)
					go w.runTask() // Запускаем задачу в отдельной горутине.
				}
			}
		}
	}()

	return nil
}

// runTask runs the task and signals when it's done.
func (w *worker) runTask() {
	defer func() {
		<-w.concurrentCh // Освобождаем место в канале.
		w.taskWg.Done()
	}()

	defer func() {
		if r := recover(); r != nil {
			slog.Error("Паника в фоновом обработчике", "panic", r)
		}
	}()
	w.task(w.ctx)
}

// Stop stops the worker and waits for all tasks to complete.
func (w *worker) Stop() error {
	w.stopChMutex.Lock()
	defer w.stopChMutex.Unlock()

	if w.stopCh == nil {
		return nil // Работник уже остановлен.
	}

	close(w.stopCh)
	<-w.loopDone
	w.taskWg.Wait()

	w.stopCh = nil
	return nil
}
