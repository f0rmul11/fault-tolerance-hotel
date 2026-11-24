package outbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/lo"

	"review/internal/pkg/event"
	"review/internal/pkg/outbox/message"
	"review/internal/pkg/outbox/storage"
	"review/internal/pkg/telemetry/traces"
	"review/internal/pkg/transaction"
)

// Topic название топика в Kafka
type Topic string

// Handler обработчик pending сообщений (отправка в kafka)
type Handler interface {
	// Handle обработка батча сообщений
	Handle(ctx context.Context, events event.Events) error
	// BatchSize размер батча сообщений для единоразовой обработки
	BatchSize(ctx context.Context) int
	// Topic название топика для текущего хендлера
	Topic() string
}

type Outbox struct {
	storage  *storage.Storage
	handlers map[Topic]Handler
	config   Config
}

func NewOutbox(pool *pgxpool.Pool, config Config, opts ...Option) *Outbox {
	outbox := &Outbox{
		storage:  storage.NewStorage(pool),
		handlers: make(map[Topic]Handler),
		config:   config,
	}

	for _, opt := range opts {
		opt(outbox)
	}
	return outbox
}

// RegisterHandler регистрирует обработчик для конкретного топика
func (s *Outbox) RegisterHandler(handler Handler) {
	topic := handler.Topic()
	s.handlers[Topic(topic)] = handler
}

// SendMetrics обновляет метрику лага outbox.
func (s *Outbox) SendMetrics(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	lag, err := s.storage.GetTopicsLag(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Не удалось получить лаг outbox", "error", err)
		return
	}
	for topic := range s.handlers {
		if _, ok := lag[string(topic)]; !ok {
			lag[string(topic)] = 0
		}
	}
	setOutboxMessagesLag(lag)
}

// Flush кладет сообщение в outbox таблицу для последующей отправки в брокер
func (s *Outbox) Flush(ctx context.Context, events event.Events) (err error) {
	ctx, span := traces.Start(ctx, "outbox.Flush")
	defer span.End()
	defer func() {
		traces.RecordError(span, err)
	}()

	var messages = make(message.Messages, 0, len(events))

	for _, ev := range events {
		messages = append(messages, message.NewMessage(
			ev.EntityID,
			ev.Schema,
			ev.Key,
			ev.Body,
			message.WithHeaders(json.RawMessage(ev.Headers)),
		))
	}

	if len(messages) == 0 {
		return nil
	}

	if err = s.storage.SaveMessages(ctx, messages); err != nil {
		return err
	}

	for _, msg := range messages {
		incOutboxMessagesWritten(msg.GetTopic())
	}
	return nil
}

// HandlePendingMessages обработка pending сообщений
func (s *Outbox) HandlePendingMessages(ctx context.Context) {
	err := transaction.Exec(ctx, func(ctx context.Context) error {
		var (
			lockLimit     = s.config.LockedKeysLimit
			messagesLimit = s.config.MessagesLimit

			wg sync.WaitGroup
		)

		pending, err := s.storage.GetPendingMessages(ctx, lockLimit, messagesLimit)
		if err != nil {
			return err
		}

		if pending.IsEmpty() {
			return nil
		}

		// группируем сообщения по топику для конкурентной обработки
		messagesByTopic := lo.GroupBy(pending, func(m *message.Message) string {
			return m.GetTopic()
		})

		// берем lock на сообщения в рамках топика и обрабатываем конкурентно
		for topic, messages := range messagesByTopic {
			wg.Add(1)
			go func(topic string, messages message.Messages) {
				defer wg.Done()
				// обрабатываем сообщения в топике (последовательно)
				s.processMessagesByTopic(ctx, topic, messages)
			}(
				topic,
				messages,
			)
		}
		wg.Wait()

		return s.storage.MarkAsProcessed(ctx, pending)
	})
	if err != nil {
		slog.ErrorContext(ctx, "Ошибка обработки outbox", "error", err)
	}
}

func (s *Outbox) processMessagesByTopic(ctx context.Context, topic string, messages message.Messages) {
	// получаем обработчик
	handler := s.handlers[Topic(topic)]
	if handler == nil {
		return
	}

	messagesErrorsLimit := s.config.MaxErrCountForMessage

	// разбиваем сообщения на чанки для обработки
	chunks := lo.Chunk(messages, handler.BatchSize(ctx))

	// обрабатываем чанки синхронно (важен порядок сообщений)
	var chunk message.Messages
	for _, chunk = range chunks {
		// обработка
		err := handler.Handle(ctx, chunk.Convert())

		// переходим к следующему чанку, если ошибок нет
		if err == nil {
			chunk.MarkAsProcessed()
			incOutboxMessagesProcessed(topic, float64(len(chunk)))
			continue
		}

		chunk.AnErrorOccurred(err)
		if lo.ContainsBy(chunk, func(message *message.Message) bool {
			return message.GetErrorsCount() >= int32(messagesErrorsLimit)
		}) {
			incCanNotHandleMessageByOutbox(topic)
		}

		hasMessageWithErrLimit := lo.ContainsBy(chunk, func(message *message.Message) bool {
			return message.GetErrorsCount() == int32(messagesErrorsLimit)
		})

		// Один лог при достижении порога, без повторов на каждой попытке.
		if hasMessageWithErrLimit {
			slog.ErrorContext(
				ctx,
				"Превышен порог ошибок отправки outbox",
				"topic",
				topic,
				"error",
				err,
			)
		}
	}
}
