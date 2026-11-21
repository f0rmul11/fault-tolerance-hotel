package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"booking/internal/pkg/event"
	"booking/internal/pkg/outbox/message"
	"booking/internal/pkg/outbox/storage"
	"booking/internal/pkg/telemetry/traces"
	"booking/internal/pkg/transaction"
	"booking/internal/pkg/transaction/abstract"
)

// Handler задаёт правила записи и отправки событий одного топика.
type Handler interface {
	Topic() string
	BatchSize(ctx context.Context) int
	PartitionsCount() int32

	// Partition возвращает явный номер или false для маршрутизации по ключу.
	Partition() (int32, bool)

	// HandlePartition возвращает nil после подтверждения всей пачки брокером.
	HandlePartition(ctx context.Context, partitionID int32, events event.Events) error
}

type Outbox struct {
	storage  *storage.Storage
	handlers map[string]Handler
	config   Config

	relaySlots chan struct{}
}

func NewOutbox(pool *pgxpool.Pool, config Config) *Outbox {
	registerMetrics()

	// Пока воркеры ждут Kafka, бизнес-транзакциям тоже нужны соединения с БД.
	parallelism := 1
	if pool != nil {
		parallelism = max(1, int(pool.Stat().MaxConns())-1)
	}

	return &Outbox{
		storage:    storage.NewStorage(pool),
		handlers:   make(map[string]Handler),
		config:     config,
		relaySlots: make(chan struct{}, parallelism),
	}
}

// RegisterHandler вызывают до запуска воркера и приёма запросов.
func (s *Outbox) RegisterHandler(handler Handler) {
	topic := handler.Topic()
	s.handlers[topic] = handler

	// Начальные значения счётчиков нужны для rate() при первой отправке.
	messagesWritten.WithLabelValues(topic)
	messagesProcessed.WithLabelValues(topic)
	failedBatches.WithLabelValues(topic)
}

// Flush сохраняет события в транзакции вызывающей стороны. Выбранная здесь
// партиция используется при каждой попытке отправки, в том числе после перезапуска.
func (s *Outbox) Flush(ctx context.Context, events event.Events) (err error) {
	ctx, span := traces.Start(ctx, "outbox.Flush")
	defer span.End()
	defer func() {
		traces.RecordError(span, err)
	}()

	messages := make([]message.Message, len(events))
	for i, ev := range events {
		handler, ok := s.handlers[ev.Schema]
		if !ok {
			return fmt.Errorf("для топика %q не зарегистрирован обработчик outbox", ev.Schema)
		}

		count := handler.PartitionsCount()
		if count <= 0 {
			return fmt.Errorf("число партиций топика %q должно быть положительным", ev.Schema)
		}

		partitionID, manual := handler.Partition()
		if !manual {
			partitionID = partitionByKey(ev.Key, count)
		}
		if partitionID < 0 || partitionID >= count {
			return fmt.Errorf("партиция %d вне диапазона 0..%d топика %q", partitionID, count-1, ev.Schema)
		}

		messages[i] = message.FromEvent(ev, partitionID)
	}

	if err := s.storage.SaveMessages(ctx, messages); err != nil {
		return err
	}

	for _, msg := range messages {
		messagesWritten.WithLabelValues(msg.Topic).Inc()
	}

	return nil
}

// HandlePendingMessages обрабатывает за тик не более одной пачки на партицию.
func (s *Outbox) HandlePendingMessages(ctx context.Context) {
	if abstract.TxFromContext(ctx) != nil {
		slog.ErrorContext(ctx, "Воркер outbox вызван внутри транзакции")
		return
	}

	var workers sync.WaitGroup
	defer workers.Wait()

	for _, handler := range s.handlers {
		for partitionID := range handler.PartitionsCount() {
			// Берём слот до запуска горутины. Лимит общий для пересекающихся тиков.
			select {
			case <-ctx.Done():
				return
			case s.relaySlots <- struct{}{}:
			}

			workers.Go(func() {
				defer func() {
					<-s.relaySlots
				}()

				s.handlePartition(ctx, handler, partitionID)
			})
		}
	}
}

func (s *Outbox) handlePartition(ctx context.Context, handler Handler, partitionID int32) {
	defer func() {
		if panicValue := recover(); panicValue != nil {
			slog.ErrorContext(ctx, "Паника в обработчике outbox",
				"topic", handler.Topic(), "partition", partitionID, "panic", panicValue,
			)
		}
	}()

	topic := handler.Topic()
	limit := min(s.config.MessagesLimit, handler.BatchSize(ctx))
	processed := 0

	// Блокировка курсора действует до завершения этой транзакции, включая отправку в Kafka.
	err := transaction.Exec(ctx, func(txCtx context.Context) error {
		page, err := s.storage.GetPendingMessages(txCtx, topic, partitionID, limit)
		if err != nil {
			return err
		}
		if len(page.Messages) == 0 {
			return nil
		}

		batch := make(event.Events, len(page.Messages))
		for i, pending := range page.Messages {
			batch[i] = event.Event{
				Schema:  pending.Topic,
				Key:     pending.Key,
				Body:    pending.Body,
				Headers: pending.Headers,
			}
		}

		if err := handler.HandlePartition(txCtx, partitionID, batch); err != nil {
			failedBatches.WithLabelValues(topic).Inc()
			return err
		}

		// Kafka подтвердила всю страницу. Сохраняем позицию последнего подтверждённого события.
		processed = len(batch)
		lastOffset := page.Messages[processed-1].Offset

		return s.storage.AdvanceOffset(txCtx, topic, partitionID, page.Start, lastOffset)
	})

	if err != nil {
		if ctx.Err() == nil {
			slog.ErrorContext(ctx, "Ошибка обработки outbox",
				"topic", topic, "partition", partitionID, "error", err,
			)
		}
		return
	}

	// Отмечаем обработку в метриках после успешного коммита курсора.
	if processed > 0 {
		messagesProcessed.WithLabelValues(topic).Add(float64(processed))
	}
}
