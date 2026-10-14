package inbox

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"check-in/internal/pkg/inbox/message"

	"github.com/samber/lo"
)

func (i *Inbox) HandlePendingMessages(mode Mode) func(ctx context.Context) {
	return func(ctx context.Context) {
		var wg sync.WaitGroup
		wg.Add(len(i.handlers))

		for _, handler := range i.handlers {
			go func(handler Handler) {
				defer wg.Done()
				err := i.handleTopicMessages(ctx, handler, mode)
				if err != nil {
					slog.Error("error while processing pending inbox messages",
						"topic", handler.Topic())
				}
			}(handler)
		}

		wg.Wait()
	}
}

func (i *Inbox) handleTopicMessages(ctx context.Context, handler Handler, mode Mode) (err error) {
	var (
		topic         = handler.Topic()
		messagesLimit = i.config.MessagesLimit
		batch         message.Messages
	)

	switch mode {
	case ModeNormal:
		batch, err = i.storage.GetPendingMessages(ctx, topic, messagesLimit)
	case ModeError:
		batch, err = i.storage.GetBrokenMessages(ctx, topic, messagesLimit)
	}

	if err != nil {
		return err
	}

	var (
		wg   sync.WaitGroup
		lock sync.Mutex
	)

	// обработанные сообщения
	processed := make(message.Messages, 0, len(batch))
	// группируем по сущности, чтобы соблюсти порядок
	grouped := lo.GroupBy(batch, func(message *message.Message) string {
		return message.GetEntityID()
	})

	// конкурентно обрабатываем батчи (порядок соблюден)
	for _, group := range grouped {
		wg.Add(1)
		go func(group message.Messages) {
			defer wg.Done()
			// обрабатываем пачку
			done := i.handleGroup(ctx, handler, group)
			// добавляем к обработанным сообщениям
			lock.Lock()
			processed = append(processed, done...)
			lock.Unlock()
		}(group)
	}
	wg.Wait()

	// если ничего не обработали - выходим
	if processed.IsEmpty() {
		return nil
	}
	// помечаем как обработанные
	if err = i.storage.MarkAsProcessed(ctx, processed); err == nil {
		incInboxMessagesProcessed(topic, float64(len(processed.Succeed())))
	}
	return err
}

func (i *Inbox) handleGroup(ctx context.Context, handler Handler, messages message.Messages) message.Messages {
	var (
		messagesErrorsLimit = i.config.MaxErrCountForMessage
		processed           = make(message.Messages, 0, len(messages))
	)

	var chunk message.Messages
	for _, chunk = range lo.Chunk(messages, handler.BatchSize(ctx)) {
		err := handleMessages(ctx, handler, chunk)
		// обработка
		if err == nil {
			// все штатно и без происшествий
			chunk.MarkAsProcessed()
			processed = append(processed, chunk...)
			continue
		}

		slog.Error(fmt.Sprintf("[inbox] can not process batch of messages due to error: %s", err.Error()),
			"topic", handler.Topic())

		// помечаем как неуспешный
		chunk.AnErrorOccurred(err)

		hasMessageWithErrLimit := lo.ContainsBy(chunk, func(message *message.Message) bool {
			return message.GetErrorsCount() >= int32(messagesErrorsLimit)
		})

		// если превысили максимальное значение -> стреляет alert
		if hasMessageWithErrLimit {
			incCanNotHandleMessageByInbox(handler.Topic())
		}
		// помечаем группу как обработанный и выходим из цикла, чтобы соблюсти порядок
		processed = append(processed, chunk...)
		break
	}

	return processed
}
func handleMessages(ctx context.Context, h Handler, messages message.Messages) (err error) {
	defer func() {
		if r := recover(); r != nil {
			incPanicHandleMessageByInbox(h.Topic())
			err = fmt.Errorf("panic while processing topic: %s. Message ID: %s.  Trace: %s", h.Topic(),
				strings.Join(messages.CorrelationIDs(), ","), r)
		}
	}()
	return h.Handle(ctx, messages)
}
