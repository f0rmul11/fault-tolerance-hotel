package outbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/lo"

	"booking/internal/pkg/event"
	"booking/internal/pkg/outbox/message"
	"booking/internal/pkg/outbox/storage"
	"booking/internal/pkg/telemetry/traces"
	"booking/internal/pkg/transaction"
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

	errorMu   sync.Mutex
	lastError error
}

func NewOutbox(pool *pgxpool.Pool, config Config) *Outbox {
	outbox := &Outbox{
		storage:  storage.NewStorage(pool),
		handlers: make(map[Topic]Handler),
		config:   config,
	}

	return outbox
}

// RegisterHandler регистрирует обработчик для конкретного топика
func (s *Outbox) RegisterHandler(handler Handler) {
	topic := handler.Topic()
	s.handlers[Topic(topic)] = handler
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

	err = s.storage.SaveMessages(ctx, messages)
	if err != nil {
		return err
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
			}(topic, messages)
		}
		wg.Wait()

		return s.storage.MarkAsProcessed(ctx, pending)
	})
	s.logProcessingError(ctx, err)
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
			continue
		}

		chunk.AnErrorOccurred(err)
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

// logProcessingError сообщает о смене ошибки, не повторяя её на каждом опросе.
func (s *Outbox) logProcessingError(ctx context.Context, err error) {
	s.errorMu.Lock()
	defer s.errorMu.Unlock()

	if err == nil {
		s.lastError = nil
		return
	}
	if s.lastError != nil && s.lastError.Error() == err.Error() {
		return
	}
	s.lastError = err
	slog.ErrorContext(ctx, "Ошибка обработки outbox", "error", err)
}
