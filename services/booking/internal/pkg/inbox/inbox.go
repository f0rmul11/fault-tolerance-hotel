package inbox

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/lo"

	"booking/internal/pkg/inbox/message"
	"booking/internal/pkg/inbox/storage"
	"booking/internal/pkg/telemetry/traces"
)

type Mode int8

const (
	ModeNormal Mode = 0
	ModeError  Mode = 1
)

// Topic название топика для текущего хендлера
type Topic string

// Handler обработчик сообщения
type Handler interface {
	// Handle обработка сообщения
	Handle(ctx context.Context, message message.Messages) error
	// BatchSize размер батча для обработки.
	// В случае, когда размер батча > 1,то при ошибке весь батч помечается как ошибочный и будет retry
	BatchSize(ctx context.Context) int
	// Topic название топика для текущего хендлера
	Topic() string
}

type Inbox struct {
	storage  *storage.Storage
	handlers map[Topic]Handler
	config   Config
}

func NewInbox(pool *pgxpool.Pool, config Config, opts ...Option) *Inbox {
	inbox := &Inbox{
		storage:  storage.NewStorage(pool),
		handlers: make(map[Topic]Handler),
		config:   config,
	}

	for _, opt := range opts {
		opt(inbox)
	}

	return inbox
}

// RegisterHandler sets handler for topic
func (i *Inbox) RegisterHandler(handler Handler) {
	topic := handler.Topic()
	i.handlers[Topic(topic)] = handler
}

func (i *Inbox) SaveMessages(ctx context.Context, messages message.Messages) (err error) {
	ctx, span := traces.Start(ctx, "inbox.SaveMessages")
	defer span.End()
	defer func() {
		traces.RecordError(span, err)
	}()

	if err := i.storage.SaveMessages(ctx, messages); err != nil {
		return err
	}

	lo.ForEach(messages, func(message *message.Message, _ int) {
		incInboxMessagesWritten(message.GetTopic())
	})
	return nil
}
